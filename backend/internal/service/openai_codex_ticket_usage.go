package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const (
	// OpenAICodexTicketConsumedKey 保存“用后即删”已领取票据的账本：指纹 → 失效 Unix 秒。
	// 它沿用票据前缀，因此和票池一样受账号编辑保留、私有字段隐藏与导出脱敏保护；
	// 解析票池时该键不含 state，会被自然忽略。账本只含不可逆的短指纹，不含票据或 Cookie。
	OpenAICodexTicketConsumedKey = openAICodexTicketExtraKeyPrefix + "consumed"
	// OpenAICodexTicketConsumedLimit 是单账号账本最多保留的条目数。
	// 已领取但尚未被下一次发布物理删除的票，每个模型最多只有库存容量（≤20）张。
	OpenAICodexTicketConsumedLimit = 2048

	// 账本条目在票据最晚可能失效后再保留一段时间，覆盖在途复验发布的同谱系新版本。
	codexTicketConsumptionGrace = 70 * time.Minute
	// 票据有效期上限为 24 小时；账本条目绝不保留超过该硬上限。
	codexTicketConsumptionMaxTTL    = 92 * 24 * time.Hour
	codexTicketConsumptionLocalCap  = 4096
	codexTicketConsumptionClaimWait = 2 * time.Second
	codexTicketConsumptionIDLength  = 24
)

// IsOpenAICodexTicketMetaExtraKey 识别与票池同前缀、但不是某个模型票池的服务端元数据键。
func IsOpenAICodexTicketMetaExtraKey(key string) bool {
	switch key {
	case OpenAICodexTicketHistoryKey, OpenAICodexTicketInvalidationsKey, OpenAICodexTicketConsumedKey:
		return true
	default:
		return false
	}
}

// codexTicketConsumptionID 是票据谱系的不可逆短指纹。软复验保留 state、session 与首次采集时间，
// 因此同一张票复验后的新版本仍被视为已使用，不会因刷新 Cookie 或 CapturedAt 被“复活”。
func codexTicketConsumptionID(ticket *openAICodexTicket) string {
	if ticket == nil {
		return ""
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{
		"v1",
		normalizeOpenAICodexTicketModel(ticket.Model),
		ticket.SessionID,
		ticket.CredentialMode,
		strings.TrimSpace(ticket.State),
		ticket.lineageCapturedAt().UTC().Format(time.RFC3339Nano),
	}, "\x00")))
	return hex.EncodeToString(digest[:])[:codexTicketConsumptionIDLength]
}

func validCodexTicketConsumptionID(id string) bool {
	if len(id) != codexTicketConsumptionIDLength {
		return false
	}
	for _, r := range id {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// codexTicketConsumptionUntil 覆盖票据及其在途复验版本的最长寿命，并截断到秒，便于账本以整数保存。
func codexTicketConsumptionUntil(ticket *openAICodexTicket, cfg config.OpenAICodexTicketConfig, now time.Time) time.Time {
	until := now.Add(time.Duration(max(cfg.TTLSeconds, 0)) * time.Second)
	if ticket != nil {
		for _, expires := range []time.Time{ticket.ExpiresAt, ticket.StateExpiresAt, ticket.hardExpiresAt()} {
			if expires.After(until) {
				until = expires
			}
		}
		if config.CodexTicketUsageAgedEnabled(cfg) && ticket.historicalExpires(cfg).After(until) {
			until = ticket.historicalExpires(cfg)
		}
	}
	until = until.Add(codexTicketConsumptionGrace)
	if limit := now.Add(codexTicketConsumptionMaxTTL); until.After(limit) {
		until = limit
	}
	return time.Unix(until.Unix(), 0)
}

// 本机领取索引按账号保存在撤票索引表中，使用独立键类型，不与票据撤销记录冲突。
type codexTicketConsumptionIndexKey int64

type codexTicketConsumptionIndex struct {
	mu      sync.Mutex
	entries map[string]time.Time
}

func (s *OpenAIGatewayService) codexTicketConsumptionIndex(accountID int64, create bool) *codexTicketConsumptionIndex {
	if s == nil || accountID <= 0 {
		return nil
	}
	key := codexTicketConsumptionIndexKey(accountID)
	if raw, ok := s.openaiCodexTicketRevoked.Load(key); ok {
		index, _ := raw.(*codexTicketConsumptionIndex)
		return index
	}
	if !create {
		return nil
	}
	raw, _ := s.openaiCodexTicketRevoked.LoadOrStore(key, &codexTicketConsumptionIndex{entries: make(map[string]time.Time)})
	index, _ := raw.(*codexTicketConsumptionIndex)
	return index
}

func (x *codexTicketConsumptionIndex) consumedLocked(id string, now time.Time) bool {
	until, ok := x.entries[id]
	return ok && until.After(now)
}

// mark 原子领取一张票；已被本机或已合并的账本领取时返回 false。
func (x *codexTicketConsumptionIndex) mark(id string, until, now time.Time) bool {
	if x == nil || id == "" {
		return false
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.consumedLocked(id, now) {
		return false
	}
	x.entries[id] = until
	if len(x.entries) > codexTicketConsumptionLocalCap {
		x.pruneLocked(now)
	}
	return true
}

// release 只撤回本次领取写入的条目；期间若合并了其他实例的领取记录则保留。
func (x *codexTicketConsumptionIndex) release(id string, until time.Time) {
	if x == nil || id == "" {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	if current, ok := x.entries[id]; ok && current.Equal(until) {
		delete(x.entries, id)
	}
}

func (x *codexTicketConsumptionIndex) merge(entries map[string]time.Time, now time.Time) {
	if x == nil || len(entries) == 0 {
		return
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	for id, until := range entries {
		if until.After(now) && until.After(x.entries[id]) {
			x.entries[id] = until
		}
	}
	if len(x.entries) > codexTicketConsumptionLocalCap {
		x.pruneLocked(now)
	}
}

// pruneLocked 先删过期条目，仍超限时淘汰最早失效的条目（对应最早被领取、早已从库存删除的票）。
func (x *codexTicketConsumptionIndex) pruneLocked(now time.Time) {
	for id, until := range x.entries {
		if !until.After(now) {
			delete(x.entries, id)
		}
	}
	if len(x.entries) <= codexTicketConsumptionLocalCap {
		return
	}
	ids := make([]string, 0, len(x.entries))
	for id := range x.entries {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, b := x.entries[ids[i]], x.entries[ids[j]]
		if a.Equal(b) {
			return ids[i] < ids[j]
		}
		return a.Before(b)
	})
	for _, id := range ids[:len(ids)-codexTicketConsumptionLocalCap] {
		delete(x.entries, id)
	}
}

// parseCodexTicketConsumptionLedger 兼容数据库解码的 map、保护换绑重编码的 json.RawMessage 等形态。
func parseCodexTicketConsumptionLedger(raw any) map[string]time.Time {
	var data []byte
	switch value := raw.(type) {
	case nil:
		return nil
	case map[string]any:
		return codexTicketConsumptionEntries(value)
	case json.RawMessage:
		data = value
	case []byte:
		data = value
	default:
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		data = encoded
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var decoded map[string]any
	if err := decoder.Decode(&decoded); err != nil {
		return nil
	}
	return codexTicketConsumptionEntries(decoded)
}

func codexTicketConsumptionEntries(values map[string]any) map[string]time.Time {
	entries := make(map[string]time.Time, len(values))
	for id, value := range values {
		if !validCodexTicketConsumptionID(id) {
			continue
		}
		var seconds int64
		switch v := value.(type) {
		case float64:
			seconds = int64(v)
		case json.Number:
			parsed, err := v.Int64()
			if err != nil {
				f, ferr := v.Float64()
				if ferr != nil {
					continue
				}
				parsed = int64(f)
			}
			seconds = parsed
		case int:
			seconds = int64(v)
		case int64:
			seconds = v
		case string:
			parsed, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				continue
			}
			seconds = parsed
		default:
			continue
		}
		if seconds > 0 {
			entries[id] = time.Unix(seconds, 0)
		}
	}
	return entries
}

func codexTicketConsumptionLedgerFromAccount(account *Account, now time.Time) map[string]time.Time {
	if account == nil || account.Extra == nil {
		return nil
	}
	raw, ok := account.Extra[OpenAICodexTicketConsumedKey]
	if !ok {
		return nil
	}
	ledger := parseCodexTicketConsumptionLedger(raw)
	for id, until := range ledger {
		if !until.After(now) {
			delete(ledger, id)
		}
	}
	return ledger
}

func markCodexTicketSlotsConsumed(inventory *openAICodexTicket, ledger map[string]time.Time) {
	if len(ledger) == 0 {
		return
	}
	for _, slot := range codexTicketSlots(inventory) {
		if _, consumed := ledger[codexTicketConsumptionID(slot)]; consumed {
			slot.consumed = true
		}
	}
}

// mergeCodexTicketConsumptionLedger 把账号快照里其他实例写入的领取记录并入本机索引。
func (s *OpenAIGatewayService) mergeCodexTicketConsumptionLedger(account *Account) {
	if s == nil || account == nil || account.ID <= 0 || account.Extra == nil {
		return
	}
	if _, ok := account.Extra[OpenAICodexTicketConsumedKey]; !ok {
		return
	}
	now := time.Now()
	if ledger := codexTicketConsumptionLedgerFromAccount(account, now); len(ledger) > 0 {
		s.codexTicketConsumptionIndex(account.ID, true).merge(ledger, now)
	}
}

func codexTicketKeyAccountID(key string) (int64, bool) {
	raw, _, found := strings.Cut(key, "\x00")
	if !found {
		return 0, false
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	return id, err == nil && id > 0
}

// markCodexTicketInventoryConsumed 只标记本机副本；已用票在下一次发布时随不可用票一起被物理删除。
func (s *OpenAIGatewayService) markCodexTicketInventoryConsumed(key string, inventory *openAICodexTicket) {
	if s == nil || inventory == nil {
		return
	}
	accountID, ok := codexTicketKeyAccountID(key)
	if !ok {
		return
	}
	index := s.codexTicketConsumptionIndex(accountID, false)
	if index == nil {
		return
	}
	now := time.Now()
	index.mu.Lock()
	defer index.mu.Unlock()
	if len(index.entries) == 0 {
		return
	}
	for _, slot := range codexTicketSlots(inventory) {
		if index.consumedLocked(codexTicketConsumptionID(slot), now) {
			slot.consumed = true
		}
	}
}

// codexTicketInventoryConsumed 报告库存中与该票相同的槽位是否已被“用后即删”领取。
func codexTicketInventoryConsumed(inventory, ticket *openAICodexTicket) bool {
	for _, slot := range codexTicketSlots(inventory) {
		if slot.consumed && sameCodexTicket(slot, ticket) {
			return true
		}
	}
	return false
}

func codexTicketNewestSlot(inventory *openAICodexTicket) *openAICodexTicket {
	var latest *openAICodexTicket
	for _, slot := range codexTicketSlots(inventory) {
		if slot != nil && (latest == nil || slot.lineageCapturedAt().After(latest.lineageCapturedAt()) ||
			slot.lineageCapturedAt().Equal(latest.lineageCapturedAt()) && slot.CapturedAt.After(latest.CapturedAt)) {
			latest = slot
		}
	}
	return latest
}

// codexTicketUsageCandidates 按取票机制给出业务请求可用的候选票。
// 最新模式按首次采集时间排序；沉淀模式只保留已沉淀票，并按最老优先。
func codexTicketUsageCandidates(inventory *openAICodexTicket, account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) []*openAICodexTicket {
	if cfg.UsageMode == config.CodexTicketUsageLatestOnly {
		latest := codexTicketNewestSlot(inventory)
		if latest != nil && latest.usable(now, account, cfg) {
			return []*openAICodexTicket{latest}
		}
		return nil
	}
	var candidates []*openAICodexTicket
	for _, slot := range codexTicketSlots(inventory) {
		if slot.usable(now, account, cfg) &&
			(!config.CodexTicketUsageAgedEnabled(cfg) || !cfg.SkipSameRouteHost || !config.CodexTicketUsesCookies(cfg) ||
				strings.TrimSpace(slot.SessionID) != "") &&
			(!config.CodexTicketUsageAgedEnabled(cfg) || !cfg.SkipSameRouteHost ||
				codexTicketRouteRotationCooldownUntil(inventory, slot, now).IsZero()) {
			candidates = append(candidates, slot)
		}
	}
	if !config.CodexTicketUsageAgedEnabled(cfg) || len(candidates) == 0 {
		return candidates
	}
	minAge := time.Duration(cfg.MinTicketAgeSeconds) * time.Second
	matured := make([]*openAICodexTicket, 0, len(candidates))
	for _, slot := range candidates {
		lineage := slot.lineageCapturedAt()
		if lineage.IsZero() || now.Sub(lineage) < minAge {
			continue
		}
		matured = append(matured, slot)
	}
	sort.SliceStable(matured, func(i, j int) bool {
		return matured[i].lineageCapturedAt().Before(matured[j].lineageCapturedAt())
	})
	return matured
}

// lookupOpenAICodexTicketForUse 按取票机制返回业务票；不回退到不可用槽位，
// 使调度准入与实际注入使用同一口径。即取即用时与原注入路径的结果一致。
func (s *OpenAIGatewayService) lookupOpenAICodexTicketForUse(account *Account, model string, cfg config.OpenAICodexTicketConfig) *openAICodexTicket {
	if s == nil || account == nil || account.ID <= 0 || strings.TrimSpace(model) == "" {
		return nil
	}
	model = normalizeOpenAICodexTicketModel(model)
	key := openAICodexTicketKey(account.ID, model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	inventory := s.availableCodexTicketInventory(key, s.codexTicketInventoryLocked(account, model))
	limitCodexTicketInventory(inventory, config.CodexTicketModelCapacity(cfg, model))
	return selectOpenAICodexTicket(inventory, account, cfg, time.Now())
}

// openAICodexTicketReuseContext 让同一次上游发送的重复注入沿用构建请求时已领取的票，
// 避免“用后即删”时一次发送消耗两张票。即取即用的回执没有领取记录，原样返回 ctx。
func openAICodexTicketReuseContext(ctx context.Context, req *http.Request) context.Context {
	if req == nil {
		return ctx
	}
	receipt, _ := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	if receipt == nil || receipt.claimed == nil || receipt.account == nil {
		return ctx
	}
	return withOpenAICodexTicketReuse(ctx, receipt.account.ID, receipt.claimed)
}

// withOpenAICodexTicketWSReuse 让同一 WebSocket 连接的后续轮次沿用握手时已领取的票。
func withOpenAICodexTicketWSReuse(ctx context.Context, receipt *openAICodexTicketWSReceipt) context.Context {
	if receipt == nil || receipt.claimed == nil || receipt.account == nil {
		return ctx
	}
	return withOpenAICodexTicketReuse(ctx, receipt.account.ID, receipt.claimed)
}

type codexTicketTurnAdmissionKey struct{}

// withCodexTicketTurnAdmission 标记“选号后、发送前”的逐轮准入。用后即删时由实际领取做权威判定，
// 否则领取后的复核会把刚领走最后一张票的请求自己拦下。
func withCodexTicketTurnAdmission(ctx context.Context) context.Context {
	return context.WithValue(ctx, codexTicketTurnAdmissionKey{}, true)
}

func codexTicketTurnAdmission(ctx context.Context) bool {
	marked, _ := ctx.Value(codexTicketTurnAdmissionKey{}).(bool)
	return marked
}

type openAICodexTicketTargetURLKey struct{}

func withOpenAICodexTicketTargetURL(ctx context.Context, target *url.URL) context.Context {
	if ctx == nil || target == nil {
		return ctx
	}
	return context.WithValue(ctx, openAICodexTicketTargetURLKey{}, target)
}

func codexTicketTargetURLFrom(ctx context.Context) *url.URL {
	target, _ := ctx.Value(openAICodexTicketTargetURLKey{}).(*url.URL)
	return target
}

type codexTicketConsumptionClaimer interface {
	ClaimCodexTicketConsumption(ctx context.Context, accountID int64, fingerprint string, expiresAt, now time.Time, limit int) (bool, error)
}

// claimCodexTicketConsumption 在共享账本中原子登记领取；false 表示其他实例已先领取。
func (s *OpenAIGatewayService) claimCodexTicketConsumption(ctx context.Context, accountID int64, id string, until, now time.Time) (bool, error) {
	repo, ok := s.accountRepo.(codexTicketConsumptionClaimer)
	if !ok {
		return true, nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), codexTicketConsumptionClaimWait)
	defer cancel()
	return repo.ClaimCodexTicketConsumption(ctx, accountID, id, until, now, OpenAICodexTicketConsumedLimit)
}

func (s *OpenAIGatewayService) codexTicketCandidatesForClaim(key string, account *Account, model string, cfg config.OpenAICodexTicketConfig) []*openAICodexTicket {
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	inventory := s.availableCodexTicketInventory(key, s.codexTicketInventoryLocked(account, model))
	limitCodexTicketInventory(inventory, config.CodexTicketModelCapacity(cfg, model))
	candidates := codexTicketUsageCandidates(inventory, account, cfg, time.Now())
	result := make([]*openAICodexTicket, 0, len(candidates))
	for _, candidate := range candidates {
		selected := codexTicketLeaf(candidate)
		if config.CodexTicketUsageAgedEnabled(cfg) {
			selected.historicalExpiresAt = selected.historicalExpires(cfg)
		}
		result = append(result, selected)
	}
	return result
}

// reuseClaimedOpenAICodexTicket 只接受同账号、同模型、仍可用且未撤销的已领取票；否则交给新领取。
func (s *OpenAIGatewayService) reuseClaimedOpenAICodexTicket(ctx context.Context, account *Account, model string, cfg config.OpenAICodexTicketConfig, h http.Header) (*openAICodexTicketReceipt, bool) {
	reuse, _ := ctx.Value(openAICodexTicketReuseKey{}).(*openAICodexTicketReuse)
	if reuse == nil || reuse.ticket == nil || reuse.accountID != account.ID || reuse.ticket.AccountID != account.ID ||
		normalizeOpenAICodexTicketModel(reuse.ticket.Model) != model {
		return nil, false
	}
	leaf := codexTicketLeaf(reuse.ticket)
	leaf.consumed = false
	if !leaf.usable(time.Now(), account, cfg) || s.codexTicketRevoked(openAICodexTicketKey(account.ID, model), leaf) {
		return nil, false
	}
	projected, err := s.prepareCodexCookieTicket(ctx, account, leaf, cfg)
	if err != nil || projected == nil || !projected.usable(time.Now(), account, cfg) {
		return nil, false
	}
	if target := codexTicketTargetURLFrom(ctx); target != nil && projected.usesCookies() && len(projected.rawCookiesForURL(target)) == 0 {
		return nil, false
	}
	projected.applyHeaders(h)
	return &openAICodexTicketReceipt{account: cloneOpenAICodexTicketAccount(account), ticket: *projected, config: cfg, service: s, claimed: leaf}, true
}

// applyConsumedOpenAICodexTicket 实现“用后即删”：先在本机索引原子领取，再在共享账本登记，
// 成功后才注入请求头。领取过的票在本机与其他实例都不再被业务选中，下一次发布时从库存删除。
func (s *OpenAIGatewayService) applyConsumedOpenAICodexTicket(ctx context.Context, account *Account, model string, cfg config.OpenAICodexTicketConfig, h http.Header) (*openAICodexTicketReceipt, error) {
	if receipt, ok := s.reuseClaimedOpenAICodexTicket(ctx, account, model, cfg, h); ok {
		return receipt, nil
	}
	unavailable := func() (*openAICodexTicketReceipt, error) {
		if cfg.FailClosed {
			return nil, ErrOpenAICodexTicketUnavailable
		}
		return nil, nil
	}
	key := openAICodexTicketKey(account.ID, model)
	target := codexTicketTargetURLFrom(ctx)
	index := s.codexTicketConsumptionIndex(account.ID, true)
	for _, leaf := range s.codexTicketCandidatesForClaim(key, account, model, cfg) {
		if config.CodexTicketUsageAgedEnabled(cfg) && cfg.SkipSameRouteHost && config.CodexTicketUsesCookies(cfg) {
			selected, err := s.selectHistoricalRouteTicket(ctx, account, model, cfg)
			if err != nil || selected == nil {
				return unavailable()
			}
			if codexTicketConsumptionID(selected) != codexTicketConsumptionID(leaf) {
				continue
			}
			leaf = selected
		}
		if s.codexTicketRevoked(key, leaf) || !leaf.usable(time.Now(), account, cfg) {
			continue
		}
		id, now := codexTicketConsumptionID(leaf), time.Now()
		projected, err := s.prepareCodexCookieTicket(ctx, account, leaf, cfg)
		if err != nil {
			return nil, err
		}
		if projected == nil || !projected.usable(time.Now(), account, cfg) {
			return unavailable()
		}
		if target != nil && projected.usesCookies() && len(projected.rawCookiesForURL(target)) == 0 {
			// 本次目标地址用不上该票的 Cookie，撤回本机领取，不消耗这张票。
			return unavailable()
		}
		if config.CodexTicketUsageAgedEnabled(cfg) {
			if !s.markHistoricalCodexTicketUsed(ctx, account, leaf, cfg) {
				return unavailable()
			}
			projected.HistoricalUsedAt = leaf.HistoricalUsedAt
			projected.historicalExpiresAt = projected.historicalExpires(cfg)
		}
		until := codexTicketConsumptionUntil(leaf, cfg, now)
		if !index.mark(id, until, now) {
			continue
		}
		claimed, err := s.claimCodexTicketConsumption(ctx, account.ID, id, until, time.Now())
		if err != nil {
			// 账本暂不可写时保留本机领取继续服务；跨实例去重在账本恢复后继续生效。
			logger.L().Warn("openai_codex_ticket consumption ledger unavailable; using local claim",
				zap.Int64("account_id", account.ID), zap.String("model", model), zap.Error(err))
		} else if !claimed {
			continue
		}
		projected.applyHeaders(h)
		return &openAICodexTicketReceipt{account: cloneOpenAICodexTicketAccount(account), ticket: *projected, config: cfg, service: s, claimed: leaf}, nil
	}
	if cfg.FailClosed {
		// 并发请求领完了票：按准入失败处理（503 可重试），不计入账号健康失败。
		return nil, denyOpenAITicket()
	}
	return nil, nil
}
