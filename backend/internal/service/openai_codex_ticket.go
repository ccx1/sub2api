package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/tidwall/gjson"
	"go.uber.org/zap"
)

const (
	openAICodexTicketExtraKeyPrefix  = "codex_turn_ticket:"
	OpenAICodexTicketEnabledExtraKey = "codex_ticket_enabled"
	openAICodexAstraMinVersion       = "0.153.4"
	openAICodexTicketStatePrefix     = "gAAAAA"
	openAICodexTicketDefaultModel    = "gpt-6-astra"
	openAICodexTicketDefaultSolModel = "gpt-5.6-sol"
)

// ErrOpenAICodexTicketUnavailable 表示该号该模型没有符合套餐规则的门票，
// 且 fail_closed 禁止裸打业务请求。
var ErrOpenAICodexTicketUnavailable = errors.New("codex turn-state ticket unavailable")

type openAICodexTicket struct {
	AttemptID           string                   `json:"attempt_id,omitempty"`
	Invalidation        *CodexTicketInvalidation `json:"invalidation,omitempty"`
	Revoked             bool                     `json:"revoked,omitempty"`
	Verified            bool                     `json:"verified,omitempty"`
	VerificationSkipped bool                     `json:"verification_skipped,omitempty"`
	Standby             *openAICodexTicket       `json:"standby,omitempty"`
	Reserve             []*openAICodexTicket     `json:"reserve,omitempty"`
	Binding             string                   `json:"binding,omitempty"`
	AccountBinding      string                   `json:"account_binding,omitempty"`
	Egress              string                   `json:"egress,omitempty"`
	HarvestProxyID      int64                    `json:"harvest_proxy_id,omitempty"`
	HarvestProxyName    string                   `json:"harvest_proxy_name,omitempty"`
	HarvestEgress       string                   `json:"harvest_egress,omitempty"`
	SessionID           string                   `json:"session_id,omitempty"`
	AccountID           int64                    `json:"account_id"`
	Model               string                   `json:"model"`
	State               string                   `json:"state"`
	CredentialMode      string                   `json:"credential_mode,omitempty"`
	// 发送投影仅属于已验证的运行时 receipt，不改变库存原票和撤销身份。
	CookieMode           string         `json:"-"`
	CookiePolicyVerified bool           `json:"-"`
	CookiePolicyProofKey string         `json:"-"`
	Cookies              []*http.Cookie `json:"cookies,omitempty"`
	CookieSessionKeys    []string       `json:"cookie_session_keys,omitempty"`
	Length               int            `json:"length"`
	CapturedAt           time.Time      `json:"captured_at"`
	// OriginCapturedAt 记录同一条凭据谱系首次采集的时间。软复验会刷新 CapturedAt
	// （Cookie 票每约 20s 一次），但谱系保持不变；质量检测与路由状态据此判定，
	// 避免每轮复验都把已完成的检测重置为“未检测”。
	OriginCapturedAt time.Time `json:"origin_captured_at,omitempty"`
	ExpiresAt        time.Time `json:"expires_at"`
	// IssuedAt and StateExpiresAt are derived from the STATE protocol metadata.
	// RevalidateAt is the configured soft refresh deadline; it must not make a
	// still-live STATE unusable.
	IssuedAt       time.Time `json:"issued_at,omitempty"`
	StateExpiresAt time.Time `json:"state_expires_at,omitempty"`
	RevalidateAt   time.Time `json:"revalidate_at,omitempty"`
	RevalidatedAt  time.Time `json:"revalidated_at,omitempty"`
	Attempts       int       `json:"attempts"`
}

func openAICodexTicketKey(accountID int64, model string) string {
	return fmt.Sprintf("%d\x00%s", accountID, strings.TrimSpace(model))
}

func openAICodexTicketExtraKey(model string) string {
	return openAICodexTicketExtraKeyPrefix + strings.TrimSpace(model)
}

func normalizeOpenAICodexTicketModel(model string) string {
	return strings.TrimSpace(model)
}

func extractOpenAICodexTicketModel(body []byte) string {
	return normalizeOpenAICodexTicketModel(gjson.GetBytes(body, "model").String())
}

func (s *OpenAIGatewayService) openAICodexTicketConfig() config.OpenAICodexTicketConfig {
	return s.openAICodexTicketConfigContext(context.Background())
}

func (s *OpenAIGatewayService) openAICodexTicketConfigContext(ctx context.Context) config.OpenAICodexTicketConfig {
	cfg := config.OpenAICodexTicketConfig{}
	if s != nil && s.cfg != nil {
		cfg = s.cfg.Gateway.OpenAICodexTicket
	}
	cfg = config.NormalizeOpenAICodexTicketConfig(cfg)
	if s != nil && s.settingService != nil {
		cfg = s.settingService.GetOpenAICodexTicketRuntimeConfig(ctx, cfg)
	}
	return cloneCodexTicketSettings(cfg)
}

func (s *OpenAIGatewayService) openAICodexTicketGatedModel(model string) bool {
	return s.openAICodexTicketGatedModelContext(context.Background(), model)
}

func (s *OpenAIGatewayService) openAICodexTicketGatedModelContext(ctx context.Context, model string) bool {
	return codexTicketConfigGatesModel(s.openAICodexTicketConfigContext(ctx), model)
}

// OpenAICodexTicketAccountEnabled returns the account-level participation
// switch. Missing values default to enabled for upgrade compatibility.
func OpenAICodexTicketAccountEnabled(account *Account) bool {
	if !isOpenAICodexTicketAccount(account) {
		return false
	}
	if account.Extra == nil {
		return true
	}
	raw, exists := account.Extra[OpenAICodexTicketEnabledExtraKey]
	if !exists || raw == nil {
		return true
	}
	enabled, ok := raw.(bool)
	return !ok || enabled
}

// OpenAICodexTicketStatus 是给管理端看的门票摘要，不含 state blob。
type OpenAICodexTicketStatus struct {
	CredentialState         string     `json:"credential_state"`
	RevalidateAt            *time.Time `json:"revalidate_at,omitempty"`
	RevalidationRequired    bool       `json:"revalidation_required"`
	Model                   string     `json:"model"`
	Length                  int        `json:"length,omitempty"`
	Ready                   bool       `json:"ready"`
	RemainingSeconds        int64      `json:"remaining_seconds"`
	Blocked                 bool       `json:"blocked"`
	ExpiresAt               *time.Time `json:"expires_at,omitempty"`
	PrimaryPresent          bool       `json:"primary_present"`
	PrimaryReady            bool       `json:"primary_ready"`
	PrimaryRemainingSeconds int64      `json:"primary_remaining_seconds,omitempty"`
	PrimaryExpiresAt        *time.Time `json:"primary_expires_at,omitempty"`
	PrimaryReason           string     `json:"primary_reason,omitempty"`
	StandbyReady            bool       `json:"standby_ready"`
	StandbyExpiresAt        *time.Time `json:"standby_expires_at,omitempty"`
	UsingStandby            bool       `json:"using_standby"`
	AvailableCount          int        `json:"available_count"`
	Capacity                int        `json:"capacity"`
	ReserveCount            int        `json:"reserve_count"`
	ExpiringCount           int        `json:"expiring_count"`
	NextExpiresAt           *time.Time `json:"next_expires_at,omitempty"`
	// Quality fields are the latest persisted model quality result. They are
	// deliberately a compact summary so ticket/usage views never expose test
	// prompts, tokens or transport details.
	QualityStatus    string     `json:"quality_status,omitempty"`
	QualityReason    string     `json:"quality_reason,omitempty"`
	QualityCheckedAt *time.Time `json:"quality_checked_at,omitempty"`
	// QualityPaused is true when a confirmed quality failure blocks this model
	// at the same admission gate as a missing ticket.
	QualityPaused            bool   `json:"quality_paused,omitempty"`
	RouteAffinityStatus      string `json:"route_affinity_status,omitempty"`
	RouteAffinityConnections int    `json:"route_affinity_connections,omitempty"`
	// RouteExpiresAt 来自 __oailb 解码后的 exp，仅在能解析时返回。
	RouteExpiresAt *time.Time `json:"route_expires_at,omitempty"`
}

func OpenAICodexTicketStatuses(account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) []OpenAICodexTicketStatus {
	if !cfg.Enabled || !OpenAICodexTicketAccountEnabled(account) {
		return nil
	}
	cfg = config.NormalizeOpenAICodexTicketConfig(cfg)
	models := cfg.Models
	out := make([]OpenAICodexTicketStatus, 0, len(models))
	for _, model := range models {
		model = normalizeOpenAICodexTicketModel(model)
		if model == "" || !isOpenAICodexTicketAccount(account, model) {
			continue
		}
		ticket := parseOpenAICodexTicketFromAny(0, model, nil)
		if account != nil && account.Extra != nil {
			ticket = parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[openAICodexTicketExtraKey(model)])
		}
		out = append(out, codexTicketPoolStatus(model, ticket, account, cfg, now))
	}
	return out
}

func (s *OpenAIGatewayService) openAICodexTicketEnabled() bool {
	return s.openAICodexTicketEnabledContext(context.Background())
}

func (s *OpenAIGatewayService) openAICodexTicketEnabledContext(ctx context.Context) bool {
	if s == nil {
		return false
	}
	fallback := s.cfg != nil && s.cfg.Gateway.OpenAICodexTicket.Enabled
	if s.settingService != nil {
		return s.settingService.GetOpenAICodexTicketEnabled(ctx, fallback)
	}
	return fallback
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestProxyURL() string {
	return s.openAICodexTicketHarvestProxyURLContext(context.Background())
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestProxyURLContext(ctx context.Context) string {
	if s.settingService != nil {
		if proxy := s.settingService.GetOpenAICodexTicketHarvestProxyURL(ctx); proxy != "" {
			return proxy
		}
	}
	return strings.TrimSpace(s.openAICodexTicketConfigContext(ctx).HarvestProxyURL)
}

func (t *openAICodexTicket) valid(now time.Time, targetLen int) bool {
	if t == nil || t.Revoked {
		return false
	}
	state := strings.TrimSpace(t.State)
	if len(state) != targetLen || t.Length != targetLen || !strings.HasPrefix(state, openAICodexTicketStatePrefix) {
		return false
	}
	expiresAt := t.hardExpiresAt()
	if expiresAt.IsZero() || !now.Before(expiresAt) {
		return false
	}
	return true
}

func (t *openAICodexTicket) needsRefresh(now time.Time, refreshBefore time.Duration) bool {
	if t == nil {
		return true
	}
	refreshAt := t.RevalidateAt
	if refreshAt.IsZero() {
		refreshAt = t.ExpiresAt
	}
	if refreshAt.IsZero() {
		return true
	}
	// Never schedule a refresh after the protocol hard expiry.  A ticket whose
	// configured TTL elapsed remains usable until hardExpiresAt, while the
	// scheduler is still prompted to perform a business revalidation.
	if hard := t.hardExpiresAt(); !hard.IsZero() && hard.Before(refreshAt) {
		refreshAt = hard
	}
	return !refreshAt.After(now.Add(refreshBefore))
}

func (t *openAICodexTicket) hardExpiresAt() time.Time {
	return codexTicketHardExpiry(t)
}

func parseOpenAICodexTicketFromAny(accountID int64, model string, raw any) *openAICodexTicket {
	if raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var ticket openAICodexTicket
	if err := json.Unmarshal(b, &ticket); err != nil {
		return nil
	}
	ticket.AccountID = accountID
	if strings.TrimSpace(model) != "" {
		ticket.Model = model
	}
	ticket.State = strings.TrimSpace(ticket.State)
	if ticket.Length == 0 {
		ticket.Length = len(ticket.State)
	}
	// Apply protocol expiry to the primary and every persisted standby/reserve
	// slot before pool normalization; old pool entries may predate these fields.
	hydrateCodexTicketStateExpiry(&ticket)
	if ticket.State == "" && !ticket.usesCookies() {
		return nil
	}
	normalizeCodexTicketPool(&ticket)
	return &ticket
}

// applyOpenAICodexTicket 在出站请求上覆盖 x-codex-turn-state。
// 请求路径只注入已捕获的有效门票，不现场打票；无票则返回
// ErrOpenAICodexTicketUnavailable。打票由后台 harvester 完成。
func (s *OpenAIGatewayService) applyOpenAICodexTicket(ctx context.Context, account *Account, model string, h http.Header) error {
	_, err := s.applyOpenAICodexTicketSnapshot(ctx, account, model, h)
	return err
}

func (s *OpenAIGatewayService) applyOpenAICodexTicketSnapshot(ctx context.Context, account *Account, model string, h http.Header) (*openAICodexTicketReceipt, error) {
	if s == nil || h == nil || !OpenAICodexTicketAccountEnabled(account) || !isOpenAICodexTicketAccount(account, model) {
		return nil, nil
	}
	model = normalizeOpenAICodexTicketModel(model)
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	if !codexTicketConfigGatesModel(cfg, model) {
		return nil, nil
	}
	if cfg.FailClosed && s.codexModelQualityPaused(ctx, account, model) {
		return nil, ErrOpenAICodexTicketUnavailable
	}
	ticket := s.lookupOpenAICodexTicketForConfig(account, model, cfg)
	if ticket != nil && ticket.usable(time.Now(), account, cfg) {
		projected, err := s.prepareCodexCookieTicket(ctx, account, ticket, cfg)
		if err != nil {
			return nil, err
		}
		if projected == nil || !projected.usable(time.Now(), account, cfg) {
			if cfg.FailClosed {
				return nil, ErrOpenAICodexTicketUnavailable
			}
			return nil, nil
		}
		projected.applyHeaders(h)
		return &openAICodexTicketReceipt{account: cloneOpenAICodexTicketAccount(account), ticket: *projected, config: cfg, service: s}, nil
	}
	if !cfg.FailClosed {
		return nil, nil
	}
	return nil, ErrOpenAICodexTicketUnavailable
}

// openAICodexTicketOutboundModel 预测本请求真正出站的模型名，也就是
// applyOpenAICodexTicket 注入时读到的 body.model。
//
// 调度门控与注入必须按同一个模型名判定门票。普通请求下二者同源：Forward 的
// upstreamModel 与本函数都走 resolveOpenAIAccountUpstreamModelForRequest，且
// Forward 会把 body.model 改写成该值后才注入。但 /responses/compact 例外——
// Forward 会把出站模型进一步改写为 compact 映射或 gateway.openai_compact_model
// （默认非空），此时若门控仍按客户端原始模型判定，就会把「实际出站是非门控
// 模型、根本不需要票」的 compact 请求整片误拦成不可调度。
func (s *OpenAIGatewayService) openAICodexTicketOutboundModel(account *Account, requestedModel string, requireCompact bool) string {
	if account.IsExcelBPSEnabledForModel(requestedModel) {
		return account.GetMappedModel(requestedModel)
	}
	model := strings.TrimSpace(requestedModel)
	if account == nil || model == "" {
		return model
	}
	if !account.IsOpenAI() {
		return canonicalOpenAIAccountSchedulingModel(account, model)
	}
	_, upstreamModel := resolveOpenAIForwardMappedModels(account, model, requireCompact)
	if requireCompact {
		// 与 Forward 同序：compact 兜底模型优先于普通/compact 映射结果。
		if compactModel := strings.TrimSpace(s.resolveOpenAICompactFallbackModel(account, model)); compactModel != "" {
			upstreamModel = compactModel
		}
	}
	if upstreamModel = strings.TrimSpace(upstreamModel); upstreamModel != "" {
		return upstreamModel
	}
	return model
}

// outboundModel 必须是真正会发给上游的模型名（openAICodexTicketOutboundModel），
// 不是客户端原始模型：注入侧读的是出站 body.model，两侧口径必须一致。
func (s *OpenAIGatewayService) openAICodexTicketBlocksAccount(account *Account, outboundModel string) bool {
	return s.openAICodexTicketBlocksAccountContext(context.Background(), account, outboundModel)
}

func (s *OpenAIGatewayService) openAICodexTicketBlocksAccountContext(ctx context.Context, account *Account, outboundModel string) bool {
	if ctx.Err() != nil {
		return true
	}
	if s == nil || !OpenAICodexTicketAccountEnabled(account) || !isOpenAICodexTicketAccount(account, outboundModel) {
		return false
	}
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	if !cfg.Enabled || !cfg.FailClosed {
		return false
	}
	model := normalizeOpenAICodexTicketModel(outboundModel)
	if !codexTicketConfigGatesModel(cfg, model) {
		return false
	}
	if s.codexModelQualityPaused(ctx, account, model) {
		return true
	}
	account = s.codexTicketSchedulingAccount(ctx, account)
	if account == nil {
		return true
	}
	if !OpenAICodexTicketAccountEnabled(account) || !isOpenAICodexTicketAccount(account, model) {
		return false
	}
	account = s.codexTicketAdmissionAccount(ctx, account)
	if account == nil {
		return true
	}
	if !OpenAICodexTicketAccountEnabled(account) || !isOpenAICodexTicketAccount(account, model) {
		return false
	}
	ticket := s.lookupOpenAICodexTicketForConfig(account, model, cfg)
	return !ticket.usable(time.Now(), account, cfg)
}

func jsonString(v string) string {
	b, err := json.Marshal(v)
	if err != nil {
		return `""`
	}
	return string(b)
}

func (s *OpenAIGatewayService) applyOpenAICodexTicketHarvestIdentity(h http.Header, account *Account, model string) {
	overrideUA := s.codexIdentityOverrideUA(account)
	if overrideUA != "" {
		h.Set("user-agent", overrideUA)
	}
	ensureCodexIdentityHeaders(h)
	enforceCodexIdentityHeadersWithUA(h, overrideUA)
	version := strings.TrimSpace(h.Get("version"))
	if needsOpenAICodexAstraVersion(model) && (version == "" || CompareVersions(version, openAICodexAstraMinVersion) < 0) {
		h.Set("version", openAICodexAstraMinVersion)
		ua := openai.SetCodexUserAgentVersion(h.Get("user-agent"), openAICodexAstraMinVersion)
		if ua == "" {
			ua = buildCodexCLIUserAgent(openAICodexAstraMinVersion)
			h.Set("originator", openai.CodexDefaultOriginator)
		}
		h.Set("user-agent", ua)
	}
}

func needsOpenAICodexAstraVersion(model string) bool {
	m := strings.ToLower(normalizeOpenAICodexTicketModel(model))
	return strings.Contains(m, "gpt-6") || strings.Contains(m, "astra")
}

func (s *OpenAIGatewayService) StartOpenAICodexTicketHarvester() {
	if s == nil {
		return
	}
	s.openaiCodexTicketLifecycleMu.Lock()
	defer s.openaiCodexTicketLifecycleMu.Unlock()
	if s.openaiCodexTicketStopped || s.openaiCodexTicketDone != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.openaiCodexTicketCancel = cancel
	s.openaiCodexTicketDone = done
	// Initialize the quality runtime before starting the asynchronous loops.
	// The admin "diagnose now" endpoint can be called immediately after the
	// service is constructed; without this eager state, its worker admission
	// sees a nil runtime context and reports a false capacity hit.
	s.codexModelQuality.mu.Lock()
	s.codexModelQuality.ctx = ctx
	if s.codexModelQuality.anomalies == nil {
		s.codexModelQuality.anomalies = make(map[string]time.Time)
	}
	if s.codexModelQuality.activeByAccount == nil {
		s.codexModelQuality.activeByAccount = make(map[int64]int)
	}
	s.codexModelQuality.mu.Unlock()
	go func() {
		defer close(done)
		qualityDone := make(chan struct{})
		go func() {
			defer close(qualityDone)
			s.runCodexModelQualityLoop(ctx)
		}()
		s.openAICodexTicketHarvestLoop(ctx)
		<-qualityDone
	}()
	logger.L().Info("openai_codex_ticket harvester started")
}

func (s *OpenAIGatewayService) StopOpenAICodexTicketHarvester() {
	if s == nil {
		return
	}
	s.openaiCodexTicketLifecycleMu.Lock()
	s.openaiCodexTicketStopped = true
	cancel, done := s.openaiCodexTicketCancel, s.openaiCodexTicketDone
	s.openaiCodexTicketLifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

func (s *OpenAIGatewayService) openAICodexTicketHarvestLoop(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.refreshOpenAICodexTickets(ctx)
			if ctx.Err() != nil {
				return
			}
			timer.Reset(s.codexTicketHarvestInterval(ctx))
		}
	}
}

// refreshOpenAICodexTickets probes each account/model with a missing or soon-to-expire
// ticket once. The loop waits for all probes, then waits the configured interval
// before starting the next cycle.
func (s *OpenAIGatewayService) refreshOpenAICodexTickets(ctx context.Context) {
	if s == nil || s.accountRepo == nil || ctx.Err() != nil || !s.openAICodexTicketEnabledContext(ctx) {
		return
	}
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		logger.L().Warn("openai_codex_ticket list accounts failed", zap.Error(err))
		return
	}
	cfg := s.openAICodexTicketConfigContext(ctx)
	if _, scheduled := s.accountRepo.(CodexTicketScheduler); scheduled {
		s.refreshScheduledCodexTickets(ctx, accounts, cfg)
		return
	}
	now := time.Now()
	var wg sync.WaitGroup
	slots := make(chan struct{}, cfg.HarvestConcurrency)
	probed := 0
	for i := range accounts {
		account := accounts[i]
		if !openAICodexTicketHarvestEnabled(&account) {
			continue
		}
		for _, model := range cfg.Models {
			model := normalizeOpenAICodexTicketModel(model)
			if model == "" {
				continue
			}
			if s.codexModelQualityCircuitPaused(ctx, &account, model) {
				continue
			}
			// 主备均有效且备用未临近过期时，本周期才停止采集。
			if !s.codexTicketInventoryNeedsRefresh(&account, model, cfg, now) {
				continue
			}
			token := account.GetCredential("access_token")
			if account.IsInDailyCooldown(now) || s.openAICodexTicketCooling(&account, token) || s.openAICodexTicketBackoffActive(&account, token, model) {
				continue
			}
			acc := account
			// Token/header helpers may update account metadata; each model owns its maps.
			acc.Extra = maps.Clone(account.Extra)
			acc.Credentials = maps.Clone(account.Credentials)
			select {
			case slots <- struct{}{}:
			case <-ctx.Done():
				wg.Wait()
				return
			}
			probed++
			wg.Add(1)
			go func(acc Account, model string) {
				defer wg.Done()
				defer func() { <-slots }()
				s.probeOnceOpenAICodexTicket(ctx, &acc, model)
			}(acc, model)
		}
	}
	wg.Wait()
	if probed > 0 {
		logger.L().Info("openai_codex_ticket probe cycle", zap.Int("probed", probed))
	}
}

// IsOpenAICodexTicketExtraKey identifies server-managed ticket material.
func IsOpenAICodexTicketExtraKey(key string) bool {
	return strings.HasPrefix(key, openAICodexTicketExtraKeyPrefix)
}

// MergeOpenAICodexTicketExtra preserves persisted tickets and the account-level
// participation switch, never summaries or blobs supplied by an account edit.
// The repository repeats this under the row lock so a concurrent harvest cannot
// be overwritten by a stale admin snapshot.
func MergeOpenAICodexTicketExtra(extra, current map[string]any) map[string]any {
	result := maps.Clone(extra)
	for key := range result {
		if IsOpenAICodexTicketExtraKey(key) {
			delete(result, key)
		}
	}
	for key, value := range current {
		if IsOpenAICodexTicketExtraKey(key) {
			if result == nil {
				result = make(map[string]any)
			}
			result[key] = value
		}
	}
	if result != nil {
		if raw, exists := result[OpenAICodexTicketEnabledExtraKey]; exists {
			if _, ok := raw.(bool); !ok {
				delete(result, OpenAICodexTicketEnabledExtraKey)
			}
		}
	}
	if _, exists := result[OpenAICodexTicketEnabledExtraKey]; !exists {
		if enabled, ok := current[OpenAICodexTicketEnabledExtraKey].(bool); ok {
			if result == nil {
				result = make(map[string]any)
			}
			result[OpenAICodexTicketEnabledExtraKey] = enabled
		}
	}
	return mergeCodexTicketProxyExtra(result, current)
}

// ValidateOpenAICodexTicketHarvestProxyURL validates only syntax, without making
// a network request or including credentials in validation errors.
func ValidateOpenAICodexTicketHarvestProxyURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.Opaque != "" || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return errors.New("harvest proxy must be an HTTP(S) or SOCKS5(h) URL with a host and no path, query or fragment")
	}
	switch parsed.Scheme {
	case "http", "https", "socks5", "socks5h":
	default:
		return errors.New("harvest proxy scheme must be http, https, socks5 or socks5h")
	}
	if port := parsed.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return errors.New("harvest proxy port must be between 1 and 65535")
		}
	}
	return nil
}

// MaskProxyURL never returns a stored proxy password, even for invalid legacy data.
func MaskProxyURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || ValidateOpenAICodexTicketHarvestProxyURL(raw) != nil {
		return ""
	}
	parsed, _ := url.Parse(raw)
	if parsed.User != nil {
		if _, ok := parsed.User.Password(); ok {
			parsed.User = url.UserPassword(parsed.User.Username(), "***")
		}
	}
	return parsed.String()
}

// IsMaskedProxyURL recognizes the exact password placeholder emitted by the API.
func IsMaskedProxyURL(raw string) bool {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		return false
	}
	password, ok := parsed.User.Password()
	return ok && password == "***"
}

// Credential shadows do not own tickets. Keep their existing forwarding policy
// instead of imposing a gate for a key the harvester never populates.
func isOpenAICodexTicketAccount(account *Account, upstreamModels ...string) bool {
	if account == nil || !account.IsOpenAIOAuthLike() || account.IsShadow() || account.isExcelBPSAllModelsEnabled() {
		return false
	}
	return len(upstreamModels) == 0 || !account.isExcelBPSUpstreamModelEnabled(upstreamModels[0])
}

// IsOpenAICodexTicketPrivateExtraKey also covers the retired account-level proxy
// override, whose credentials may remain in older account records.
func IsOpenAICodexTicketPrivateExtraKey(key string) bool {
	return IsOpenAICodexTicketExtraKey(key) || key == "codex_harvest_proxy_url"
}

// RedactOpenAICodexTicketExtra strips ephemeral ticket material from exports
// without changing the source account or unrelated backup fields.
func RedactOpenAICodexTicketExtra(extra map[string]any) map[string]any {
	redacted := maps.Clone(extra)
	for key := range redacted {
		if IsOpenAICodexTicketPrivateExtraKey(key) {
			delete(redacted, key)
		}
	}
	return redacted
}
