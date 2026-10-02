package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// 账号票库：管理员查看与作废已存票据。票据仍保存在 accounts.extra 的按模型票池
// （主票、备用、储备票，容量即打票配置的库存容量），票库不另建存储。
// 接口只返回元数据（状态、时间、长度、Cookie 名、单向指纹），绝不返回原始 STATE、
// Cookie 值、会话 ID、出口标识或账号凭据。

const (
	CodexTicketVaultInvalidationReason = "admin_revoked"
	CodexTicketVaultInvalidationSource = "ticket_vault"

	// 其余状态沿用票池诊断原因：revoked、consumed、binding、credential、
	// cookie_missing、expired、unverified、unavailable。
	CodexTicketVaultStatusAvailable = "available"
	CodexTicketVaultStatusMaturing  = "maturing"

	codexTicketVaultFingerprintLength = 24
	// 软复验会替换票据版本，作废按谱系指纹在最新库存上重新定位，最多重试数轮。
	codexTicketVaultRevokeRounds   = 3
	codexTicketVaultMaxModelLength = 256
)

var (
	ErrCodexTicketVaultSlotNotFound = errors.New("codex ticket vault slot not found")
	ErrCodexTicketVaultInvalidInput = errors.New("invalid codex ticket vault request")
)

type CodexTicketVaultPolicy struct {
	UsageMode           string `json:"usage_mode"`
	MinTicketAgeSeconds int    `json:"min_ticket_age_seconds"`
	ConsumeAfterUse     bool   `json:"consume_after_use"`
	FailClosed          bool   `json:"fail_closed"`
}

// CodexTicketVaultInvalidation 只保留撤销原因与时间，不含上游信号或票据内容。
type CodexTicketVaultInvalidation struct {
	Reason        string     `json:"reason"`
	Source        string     `json:"source"`
	InvalidatedAt *time.Time `json:"invalidated_at,omitempty"`
}

type CodexTicketVaultSlot struct {
	Label               string                        `json:"label"`
	Fingerprint         string                        `json:"fingerprint"`
	Status              string                        `json:"status"`
	BusinessSelected    bool                          `json:"business_selected"`
	CredentialMode      string                        `json:"credential_mode,omitempty"`
	Length              int                           `json:"length"`
	CookieNames         []string                      `json:"cookie_names"`
	Verified            bool                          `json:"verified"`
	VerificationSkipped bool                          `json:"verification_skipped"`
	CapturedAt          *time.Time                    `json:"captured_at,omitempty"`
	OriginCapturedAt    *time.Time                    `json:"origin_captured_at,omitempty"`
	AgeSeconds          int64                         `json:"age_seconds"`
	MatureAt            *time.Time                    `json:"mature_at,omitempty"`
	ExpiresAt           *time.Time                    `json:"expires_at,omitempty"`
	RemainingSeconds    int64                         `json:"remaining_seconds"`
	StateExpiresAt      *time.Time                    `json:"state_expires_at,omitempty"`
	RevalidateAt        *time.Time                    `json:"revalidate_at,omitempty"`
	RevalidatedAt       *time.Time                    `json:"revalidated_at,omitempty"`
	HarvestProxyName    string                        `json:"harvest_proxy_name,omitempty"`
	HarvestCountry      string                        `json:"harvest_country,omitempty"`
	RouteNode           *CodexTicketNodeInfo          `json:"route_node,omitempty"`
	Attempts            int                           `json:"attempts"`
	Invalidation        *CodexTicketVaultInvalidation `json:"invalidation,omitempty"`
}

type CodexTicketVaultModel struct {
	Model      string                 `json:"model"`
	Configured bool                   `json:"configured"`
	Total      int                    `json:"total"`
	Available  int                    `json:"available"`
	Maturing   int                    `json:"maturing"`
	Slots      []CodexTicketVaultSlot `json:"slots"`
}

type CodexTicketVault struct {
	AccountID        int64                   `json:"account_id"`
	AccountName      string                  `json:"account_name"`
	TicketEnabled    bool                    `json:"ticket_enabled"`
	HarvestEnabled   bool                    `json:"harvest_enabled"`
	ConfigEnabled    bool                    `json:"config_enabled"`
	ProxyAvailable   bool                    `json:"proxy_available"`
	CredentialMode   string                  `json:"credential_mode,omitempty"`
	PoolCapacity     int                     `json:"pool_capacity"`
	TTLSeconds       int                     `json:"ttl_seconds"`
	CookieTTLSeconds int                     `json:"cookie_ttl_seconds"`
	Policy           CodexTicketVaultPolicy  `json:"policy"`
	Models           []CodexTicketVaultModel `json:"models"`
	ServerTime       time.Time               `json:"server_time"`
}

// CodexTicketVaultRevokeInput 按指纹作废单张票，或 All=true 作废该模型全部未作废的票。
type CodexTicketVaultRevokeInput struct {
	Model       string `json:"model"`
	Fingerprint string `json:"fingerprint,omitempty"`
	All         bool   `json:"all,omitempty"`
}

type CodexTicketVaultRevokeResult struct {
	AccountID int64  `json:"account_id"`
	Model     string `json:"model"`
	Revoked   int    `json:"revoked"`
	// Remaining 是数据库中仍未作废的目标票数；大于 0 表示需要稍后重试。
	Remaining int `json:"remaining"`
}

// GetOpenAICodexTicketVault 返回账号各模型票池的元数据视图，口径与业务取票一致。
func (s *OpenAIGatewayService) GetOpenAICodexTicketVault(ctx context.Context, accountID int64) (CodexTicketVault, error) {
	now := time.Now()
	result := CodexTicketVault{AccountID: accountID, Models: []CodexTicketVaultModel{}, ServerTime: now.UTC()}
	account, err := s.codexTicketNodeAccount(ctx, accountID)
	if err != nil {
		return result, err
	}
	result.AccountName = account.Name
	result.TicketEnabled, result.HarvestEnabled = OpenAICodexTicketAccountEnabled(account), openAICodexTicketHarvestEnabled(account)
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	egress := s.codexTicketNodeEgress(ctx, account)
	resolved := resolveCodexTicketCredentialConfig(egress.account, cfg)
	result.ConfigEnabled, result.ProxyAvailable = cfg.Enabled, egress.available
	result.CredentialMode, result.PoolCapacity = resolved.CredentialMode, resolved.PoolCapacity
	result.TTLSeconds, result.CookieTTLSeconds = resolved.TTLSeconds, resolved.CookieTTLSeconds
	result.Policy = CodexTicketVaultPolicy{UsageMode: config.CodexTicketUsageImmediate, ConsumeAfterUse: resolved.ConsumeAfterUse, FailClosed: resolved.FailClosed}
	if config.CodexTicketUsageAgedEnabled(resolved) {
		result.Policy.UsageMode, result.Policy.MinTicketAgeSeconds = config.CodexTicketUsageAged, resolved.MinTicketAgeSeconds
	}
	for _, model := range codexTicketNodeModels(account, cfg) {
		// 库存已是副本，补齐软复验时间不影响共享缓存。
		inventory, selected := s.codexTicketNodeInventory(egress.account, model, cfg, now)
		hydrateCodexTicketSoftRevalidate(inventory, resolved)
		slots, labels := codexTicketNodeSlotLabels(inventory)
		view := CodexTicketVaultModel{Model: model, Configured: codexTicketConfigGatesModel(cfg, model), Slots: make([]CodexTicketVaultSlot, 0, len(slots))}
		for i, slot := range slots {
			item := codexTicketVaultSlotView(account.ID, model, labels[i], slot, selected, egress.account, resolved, now)
			switch item.Status {
			case CodexTicketVaultStatusAvailable:
				view.Available++
			case CodexTicketVaultStatusMaturing:
				view.Maturing++
			}
			view.Slots = append(view.Slots, item)
		}
		view.Total = len(view.Slots)
		result.Models = append(result.Models, view)
	}
	return result, nil
}

func codexTicketVaultSlotView(accountID int64, model, label string, slot, selected *openAICodexTicket, account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) CodexTicketVaultSlot {
	lineage, expires := slot.lineageCapturedAt(), slot.hardExpiresAt()
	view := CodexTicketVaultSlot{Label: label, Fingerprint: codexTicketVaultFingerprint(accountID, model, slot),
		BusinessSelected: sameCodexTicket(slot, selected), CredentialMode: slot.CredentialMode, Length: slot.Length,
		CookieNames: codexTicketVaultCookieNames(slot), Verified: slot.Verified, VerificationSkipped: slot.VerificationSkipped,
		CapturedAt: codexTicketNodeTime(slot.CapturedAt), OriginCapturedAt: codexTicketNodeTime(slot.OriginCapturedAt),
		ExpiresAt: codexTicketNodeTime(expires), StateExpiresAt: codexTicketNodeTime(slot.StateExpiresAt),
		RevalidateAt: codexTicketNodeTime(slot.RevalidateAt), RevalidatedAt: codexTicketNodeTime(slot.RevalidatedAt),
		HarvestProxyName: slot.HarvestProxyName, HarvestCountry: slot.HarvestCountry, Attempts: slot.Attempts,
		// 只取 __oailb 解出的节点信息，Cookie 值与声明本身不出接口。
		RouteNode: codexTicketNodeCookieNode(codexTicketNodeCookieViews(slot.Cookies, ""), false)}
	if !lineage.IsZero() {
		view.AgeSeconds = codexTicketVaultSeconds(now.Sub(lineage))
	}
	if !expires.IsZero() {
		view.RemainingSeconds = codexTicketVaultSeconds(expires.Sub(now))
	}
	if event := slot.Invalidation; event != nil {
		view.Invalidation = &CodexTicketVaultInvalidation{Reason: event.Reason, Source: event.Source, InvalidatedAt: codexTicketNodeTime(event.InvalidatedAt)}
	}
	aged := config.CodexTicketUsageAgedEnabled(cfg)
	minAge := time.Duration(cfg.MinTicketAgeSeconds) * time.Second
	if aged && !lineage.IsZero() {
		view.MatureAt = codexTicketNodeTime(lineage.Add(minAge))
	}
	view.Status = codexTicketPrimaryReason(slot, account, cfg, now)
	if view.Status == "" {
		// 与 codexTicketUsageCandidates 一致：沉淀模式下缺采集时间的票不会被业务选中。
		switch {
		case aged && lineage.IsZero():
			view.Status = "unavailable"
		case aged && now.Sub(lineage) < minAge:
			view.Status = CodexTicketVaultStatusMaturing
		default:
			view.Status = CodexTicketVaultStatusAvailable
		}
	}
	return view
}

// codexTicketVaultSeconds 把时长截断为非负整秒。
func codexTicketVaultSeconds(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return int64(d / time.Second)
}

func codexTicketVaultCookieNames(ticket *openAICodexTicket) []string {
	names := []string{}
	for _, cookie := range ticket.Cookies {
		if cookie != nil && cookie.Name != "" && !slices.Contains(names, cookie.Name) {
			names = append(names, cookie.Name)
		}
	}
	return names
}

// codexTicketVaultFingerprint 是票据谱系的单向短指纹：软复验刷新 CapturedAt 与 Cookie
// 时保持不变，便于管理端定位；无法从中还原 STATE 或会话。
func codexTicketVaultFingerprint(accountID int64, model string, ticket *openAICodexTicket) string {
	if ticket == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{"vault-v1", strconv.FormatInt(accountID, 10),
		normalizeOpenAICodexTicketModel(model), ticket.SessionID, ticket.CredentialMode, ticket.Egress,
		strings.TrimSpace(ticket.State), ticket.lineageCapturedAt().UTC().Format(time.RFC3339Nano)}, "\x00")))
	return hex.EncodeToString(sum[:])[:codexTicketVaultFingerprintLength]
}

func validCodexTicketVaultFingerprint(value string) bool {
	if len(value) != codexTicketVaultFingerprintLength {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// RevokeOpenAICodexTicketVault 作废票库中的指定票，或某模型全部未作废的票。
// 作废写入与业务撤票相同的墓碑（跨实例生效并记入撤销历史），票据在下一次发布时
// 从库存物理删除，之后由采集补足库存。
func (s *OpenAIGatewayService) RevokeOpenAICodexTicketVault(ctx context.Context, accountID int64, input CodexTicketVaultRevokeInput) (CodexTicketVaultRevokeResult, error) {
	model := normalizeOpenAICodexTicketModel(input.Model)
	fingerprint := strings.ToLower(strings.TrimSpace(input.Fingerprint))
	result := CodexTicketVaultRevokeResult{AccountID: accountID, Model: model}
	switch {
	case model == "" || len(model) > codexTicketVaultMaxModelLength:
		return result, fmt.Errorf("%w: model is required", ErrCodexTicketVaultInvalidInput)
	case input.All == (fingerprint != ""):
		return result, fmt.Errorf("%w: specify exactly one of fingerprint or all", ErrCodexTicketVaultInvalidInput)
	case fingerprint != "" && !validCodexTicketVaultFingerprint(fingerprint):
		return result, fmt.Errorf("%w: invalid fingerprint", ErrCodexTicketVaultInvalidInput)
	}
	account, err := s.codexTicketNodeAccount(ctx, accountID)
	if err != nil {
		return result, err
	}
	if !slices.Contains(codexTicketNodeModels(account, s.openAICodexTicketConfigForAccount(ctx, account)), model) {
		return result, ErrCodexTicketVaultSlotNotFound
	}
	// 先在持久化库存上确定目标谱系；已作废的票视为完成，重复作废不报错。
	wanted, matched := map[string]bool{}, false
	for _, slot := range codexTicketVaultPersistedSlots(account, model) {
		id := codexTicketVaultFingerprint(account.ID, model, slot)
		if input.All || id == fingerprint {
			matched = true
			if !slot.Revoked {
				wanted[id] = true
			}
		}
	}
	if !input.All && !matched {
		return result, ErrCodexTicketVaultSlotNotFound
	}
	var firstErr error
	for round := 0; ; round++ {
		targets := codexTicketVaultPendingTargets(account, model, wanted)
		result.Remaining = len(targets)
		if len(targets) == 0 || round == codexTicketVaultRevokeRounds {
			break
		}
		for _, target := range targets {
			used := codexTicketWithInvalidation(target, newCodexTicketInvalidation(CodexTicketVaultInvalidationReason, CodexTicketVaultInvalidationSource, nil))
			revoked, err := s.revokeCodexTicketVaultSlot(ctx, account, used)
			if err != nil && firstErr == nil {
				firstErr = err
			}
			if revoked {
				result.Revoked++
			}
		}
		// 重新读取数据库：确认墓碑已落库，并在票据被软复验替换时定位新版本。
		reloaded, err := s.codexTicketNodeAccount(ctx, accountID)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			result.Remaining = max(0, result.Remaining-result.Revoked)
			break
		}
		account = reloaded
	}
	if firstErr != nil && result.Revoked == 0 {
		return result, firstErr
	}
	return result, nil
}

// codexTicketVaultPersistedSlots 只解析数据库快照：作废进度以持久化状态为准，
// 不受本机撤销索引影响（写库失败时本机已阻断该票，但其他实例仍可能使用）。
func codexTicketVaultPersistedSlots(account *Account, model string) []*openAICodexTicket {
	return codexTicketSlots(parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[openAICodexTicketExtraKey(model)]))
}

func codexTicketVaultPendingTargets(account *Account, model string, wanted map[string]bool) []*openAICodexTicket {
	var targets []*openAICodexTicket
	for _, slot := range codexTicketVaultPersistedSlots(account, model) {
		if !slot.Revoked && wanted[codexTicketVaultFingerprint(account.ID, model, slot)] {
			targets = append(targets, slot)
		}
	}
	return targets
}

// revokeCodexTicketVaultSlot 与业务撤票（invalidateOpenAICodexTicketOnce）使用相同的分片锁、
// 本机撤销索引与条件写库，但把写库结果交给调用方用于重试与统计。
func (s *OpenAIGatewayService) revokeCodexTicketVaultSlot(ctx context.Context, account *Account, used *openAICodexTicket) (bool, error) {
	key := openAICodexTicketKey(account.ID, used.Model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	inventory := s.codexTicketInventoryLocked(account, used.Model)
	if !codexTicketInventoryContains(inventory, used) {
		return false, nil
	}
	// 写库失败时本机也立即停用该票；数据库墓碑由调用方按最新库存重试。
	s.rememberCodexTicketRevocation(key, used)
	snapshot := cloneOpenAICodexTicketAccount(account)
	snapshot.Extra[openAICodexTicketExtraKey(used.Model)] = inventory
	tombstone := *codexTicketLeaf(used)
	tombstone.Revoked = true
	updated, err := s.persistOpenAICodexTicketResult(context.WithoutCancel(ctx), snapshot, used.Model, &tombstone)
	if updated && err == nil {
		s.openaiCodexTickets.Store(key, revokeCodexTicketSlot(inventory, used))
	}
	return updated && err == nil, err
}
