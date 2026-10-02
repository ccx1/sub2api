package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
)

// 节点采集是管理员取证工具：只读票据库存、待定 Cookie 与 WS 连接池；探测结果只随响应返回，
// 不写票池、采集历史、质量记录，也不触发代理冷却。测试阶段原样返回 Cookie/STATE，
// 方案确认后再补脱敏。

const (
	CodexTicketNodeSourceTicket    = "ticket"
	CodexTicketNodeSourceEmptyJar  = "empty_jar"
	CodexTicketNodeSourceStickyJar = "sticky_jar"

	codexTicketNodeProbeMaxRounds = 5
	// 外层反向代理常见 60s 超时，整批探测留出余量。
	codexTicketNodeProbeBudget     = 55 * time.Second
	codexTicketNodeProbeRoundLimit = 20 * time.Second
	codexTicketNodeProbeMinRound   = 2 * time.Second
)

var (
	ErrCodexTicketNodesUnavailable = errors.New("codex ticket node capture unavailable for this account")
	ErrCodexTicketNodeProbeBusy    = errors.New("codex ticket node probe already running for this account")
)

// 同一账号同时只允许一批节点探测，避免管理端重复点击放大上游请求。
var codexTicketNodeProbeInflight sync.Map

type CodexTicketNodeProbeError struct{ Reason string }

func (e *CodexTicketNodeProbeError) Error() string {
	return "codex ticket node probe unavailable: " + e.Reason
}

type CodexTicketNodeInfo struct {
	Host        string `json:"host"`
	Recognized  bool   `json:"recognized"`
	Name        string `json:"name,omitempty"`
	Number      int    `json:"number,omitempty"`
	Country     string `json:"country,omitempty"`
	Region      string `json:"region,omitempty"`
	MacroRegion string `json:"macro_region,omitempty"`
}

type CodexTicketNodeCookie struct {
	Name           string               `json:"name"`
	Value          string               `json:"value"`
	Domain         string               `json:"domain,omitempty"`
	Path           string               `json:"path,omitempty"`
	ExpiresAt      *time.Time           `json:"expires_at,omitempty"`
	MaxAge         int                  `json:"max_age,omitempty"`
	Secure         bool                 `json:"secure,omitempty"`
	HttpOnly       bool                 `json:"http_only,omitempty"`
	SameSite       string               `json:"same_site,omitempty"`
	ExcludedByMode bool                 `json:"excluded_by_mode,omitempty"`
	Node           *CodexTicketNodeInfo `json:"node,omitempty"`
	Claims         map[string]any       `json:"claims,omitempty"`
	ClaimExpiresAt *time.Time           `json:"claim_expires_at,omitempty"`
	DecodeError    string               `json:"decode_error,omitempty"`
}

type CodexTicketNodeSlot struct {
	Label                string                   `json:"label"`
	BusinessSelected     bool                     `json:"business_selected"`
	Usable               bool                     `json:"usable"`
	Revoked              bool                     `json:"revoked"`
	Verified             bool                     `json:"verified"`
	VerificationSkipped  bool                     `json:"verification_skipped"`
	CredentialMode       string                   `json:"credential_mode,omitempty"`
	SessionID            string                   `json:"session_id,omitempty"`
	State                string                   `json:"state,omitempty"`
	StateLength          int                      `json:"state_length"`
	EgressMatchesCurrent *bool                    `json:"egress_matches_current,omitempty"`
	HarvestProxyID       int64                    `json:"harvest_proxy_id,omitempty"`
	HarvestProxyName     string                   `json:"harvest_proxy_name,omitempty"`
	HarvestCountry       string                   `json:"harvest_country,omitempty"`
	HarvestEgressDiffers bool                     `json:"harvest_egress_differs,omitempty"`
	CapturedAt           *time.Time               `json:"captured_at,omitempty"`
	OriginCapturedAt     *time.Time               `json:"origin_captured_at,omitempty"`
	ExpiresAt            *time.Time               `json:"expires_at,omitempty"`
	HardExpiresAt        *time.Time               `json:"hard_expires_at,omitempty"`
	StateExpiresAt       *time.Time               `json:"state_expires_at,omitempty"`
	RevalidateAt         *time.Time               `json:"revalidate_at,omitempty"`
	RevalidatedAt        *time.Time               `json:"revalidated_at,omitempty"`
	RouteExpiresAt       *time.Time               `json:"route_expires_at,omitempty"`
	Attempts             int                      `json:"attempts"`
	RouteNode            *CodexTicketNodeInfo     `json:"route_node,omitempty"`
	SentNode             *CodexTicketNodeInfo     `json:"sent_node,omitempty"`
	CrossRegion          bool                     `json:"cross_region"`
	RouteFingerprint     string                   `json:"route_fingerprint,omitempty"`
	Invalidation         *CodexTicketInvalidation `json:"invalidation,omitempty"`
	Cookies              []CodexTicketNodeCookie  `json:"cookies"`
}

type CodexTicketNodePending struct {
	ExpiresAt           *time.Time              `json:"expires_at,omitempty"`
	TicketLabel         string                  `json:"ticket_label,omitempty"`
	TicketNode          *CodexTicketNodeInfo    `json:"ticket_node,omitempty"`
	CandidateNode       *CodexTicketNodeInfo    `json:"candidate_node,omitempty"`
	HeaderChanged       bool                    `json:"header_changed"`
	CandidateCapturedAt *time.Time              `json:"candidate_captured_at,omitempty"`
	CandidateExpiresAt  *time.Time              `json:"candidate_expires_at,omitempty"`
	Cookies             []CodexTicketNodeCookie `json:"cookies"`
}

type CodexTicketNodeModel struct {
	Model      string                  `json:"model"`
	Configured bool                    `json:"configured"`
	Slots      []CodexTicketNodeSlot   `json:"slots"`
	Pending    *CodexTicketNodePending `json:"pending,omitempty"`
}

type CodexTicketNodeConnection struct {
	ID                        string                  `json:"id"`
	Model                     string                  `json:"model,omitempty"`
	RoutingAffinity           string                  `json:"routing_affinity,omitempty"`
	RouteFingerprint          string                  `json:"route_fingerprint,omitempty"`
	Strict                    bool                    `json:"strict"`
	HasReceipt                bool                    `json:"has_receipt"`
	SlotLabel                 string                  `json:"slot_label,omitempty"`
	CreatedAt                 *time.Time              `json:"created_at,omitempty"`
	LastUsedAt                *time.Time              `json:"last_used_at,omitempty"`
	Leased                    bool                    `json:"leased"`
	LeasedBefore              bool                    `json:"leased_before"`
	Prewarmed                 bool                    `json:"prewarmed"`
	Closed                    bool                    `json:"closed"`
	Unusable                  bool                    `json:"unusable"`
	SentNode                  *CodexTicketNodeInfo    `json:"sent_node,omitempty"`
	ReceivedNode              *CodexTicketNodeInfo    `json:"received_node,omitempty"`
	SentCookies               []CodexTicketNodeCookie `json:"sent_cookies"`
	ReceivedCookies           []CodexTicketNodeCookie `json:"received_cookies"`
	HandshakeHeaders          map[string][]string     `json:"handshake_headers"`
	HandshakeHeadersTruncated bool                    `json:"handshake_headers_truncated,omitempty"`
}

type CodexTicketNodeConfig struct {
	Enabled          bool     `json:"enabled"`
	CredentialMode   string   `json:"credential_mode,omitempty"`
	SessionMode      string   `json:"session_mode,omitempty"`
	RefreshStrategy  string   `json:"refresh_strategy,omitempty"`
	PoolCapacity     int      `json:"pool_capacity"`
	CookieTTLSeconds int      `json:"cookie_ttl_seconds"`
	TTLSeconds       int      `json:"ttl_seconds"`
	Models           []string `json:"models"`
}

type CodexTicketNodeStrategy struct {
	Enabled                 bool   `json:"enabled"`
	Applies                 bool   `json:"applies"`
	Strategy                string `json:"strategy,omitempty"`
	Scope                   string `json:"scope,omitempty"`
	RouteAffinityMode       string `json:"route_affinity_mode,omitempty"`
	RoutePrewarmConnections int    `json:"route_prewarm_connections"`
	CookieMode              string `json:"cookie_mode,omitempty"`
}

type CodexTicketNodeSnapshot struct {
	AccountID         int64                       `json:"account_id"`
	AccountName       string                      `json:"account_name"`
	AccountStatus     string                      `json:"account_status"`
	Schedulable       bool                        `json:"schedulable"`
	TicketEnabled     bool                        `json:"ticket_enabled"`
	HarvestEnabled    bool                        `json:"harvest_enabled"`
	RandomProxy       bool                        `json:"random_proxy"`
	ProxyAvailable    bool                        `json:"proxy_available"`
	Proxy             *CodexTicketProxySnapshot   `json:"proxy,omitempty"`
	EgressCountry     string                      `json:"egress_country,omitempty"`
	EgressMacroRegion string                      `json:"egress_macro_region,omitempty"`
	CookieMode        string                      `json:"cookie_mode"`
	Config            CodexTicketNodeConfig       `json:"config"`
	Strategy          CodexTicketNodeStrategy     `json:"strategy"`
	Models            []CodexTicketNodeModel      `json:"models"`
	Connections       []CodexTicketNodeConnection `json:"connections"`
	ServerTime        time.Time                   `json:"server_time"`
}

type CodexTicketNodeProbeInput struct {
	Model  string `json:"model"`
	Source string `json:"source"`
	Slot   string `json:"slot,omitempty"`
	Count  int    `json:"count"`
}

type CodexTicketNodeProbeRound struct {
	Index           int                     `json:"index"`
	Status          string                  `json:"status"`
	Reason          string                  `json:"reason,omitempty"`
	SessionID       string                  `json:"session_id,omitempty"`
	StartedAt       *time.Time              `json:"started_at,omitempty"`
	HTTPStatus      int                     `json:"http_status,omitempty"`
	HeaderLatencyMs int64                   `json:"header_latency_ms"`
	FirstByteMs     *int64                  `json:"first_byte_ms,omitempty"`
	FirstDeltaMs    *int64                  `json:"first_delta_ms,omitempty"`
	TotalLatencyMs  int64                   `json:"total_latency_ms"`
	NodeOutcome     string                  `json:"node_outcome"`
	SentNode        *CodexTicketNodeInfo    `json:"sent_node,omitempty"`
	ReceivedNode    *CodexTicketNodeInfo    `json:"received_node,omitempty"`
	EffectiveNode   *CodexTicketNodeInfo    `json:"effective_node,omitempty"`
	SentCookies     []CodexTicketNodeCookie `json:"sent_cookies"`
	ReceivedCookies []CodexTicketNodeCookie `json:"received_cookies"`
	TurnStateLength int                     `json:"turn_state_length"`
	ResponseID      string                  `json:"response_id,omitempty"`
	Exchange        *CodexTicketExchange    `json:"exchange,omitempty"`
}

type CodexTicketNodeProbeResult struct {
	AccountID     int64                       `json:"account_id"`
	Model         string                      `json:"model"`
	Source        string                      `json:"source"`
	Slot          string                      `json:"slot,omitempty"`
	SlotNode      *CodexTicketNodeInfo        `json:"slot_node,omitempty"`
	CookieMode    string                      `json:"cookie_mode"`
	Proxy         *CodexTicketProxySnapshot   `json:"proxy,omitempty"`
	EgressCountry string                      `json:"egress_country,omitempty"`
	NodeCounts    map[string]int              `json:"node_counts"`
	Rounds        []CodexTicketNodeProbeRound `json:"rounds"`
	StartedAt     time.Time                   `json:"started_at"`
	DurationMs    int64                       `json:"duration_ms"`
}

type codexTicketNodeEgress struct {
	account   *Account
	available bool
	proxyURL  string
	proxy     *CodexTicketProxySnapshot
	country   string
}

type codexTicketNodeConnCopy struct {
	id, affinity, fingerprint string
	receipt                   *openAICodexTicketWSReceipt
	headers                   http.Header
	strict, leased, leasedBefore,
	prewarmed, closed, unusable bool
	created, lastUsed time.Time
}

func (s *OpenAIGatewayService) GetOpenAICodexTicketNodes(ctx context.Context, accountID int64) (CodexTicketNodeSnapshot, error) {
	now := time.Now()
	result := CodexTicketNodeSnapshot{AccountID: accountID, Models: []CodexTicketNodeModel{}, Connections: []CodexTicketNodeConnection{}, ServerTime: now.UTC()}
	account, err := s.codexTicketNodeAccount(ctx, accountID)
	if err != nil {
		return result, err
	}
	result.AccountName, result.AccountStatus, result.Schedulable = account.Name, account.Status, account.Schedulable
	result.TicketEnabled, result.HarvestEnabled = OpenAICodexTicketAccountEnabled(account), openAICodexTicketHarvestEnabled(account)
	result.RandomProxy = account.IsRandomProxy()
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	result.Config = CodexTicketNodeConfig{Enabled: cfg.Enabled, CredentialMode: cfg.CredentialMode, SessionMode: cfg.SessionMode,
		RefreshStrategy: cfg.RefreshStrategy, PoolCapacity: cfg.PoolCapacity, CookieTTLSeconds: cfg.CookieTTLSeconds,
		TTLSeconds: cfg.TTLSeconds, Models: append([]string{}, cfg.Models...)}
	mode := s.codexCookieModeForRequest(ctx, account)
	result.CookieMode = mode
	policy, applies := s.codexRequestStrategyPolicyForScope(ctx, CodexRequestStrategyScopeDedicated)
	result.Strategy = CodexTicketNodeStrategy{Enabled: policy.Enabled, Applies: applies, Strategy: policy.Strategy, Scope: policy.Scope,
		RouteAffinityMode: policy.RouteAffinityMode, RoutePrewarmConnections: policy.RoutePrewarmConnections, CookieMode: policy.CookieMode}
	egress := s.codexTicketNodeEgress(ctx, account)
	result.ProxyAvailable, result.Proxy, result.EgressCountry = egress.available, egress.proxy, egress.country
	result.EgressMacroRegion = codexMacroRegionForCountry(egress.country)

	slotsByModel := map[string][]*openAICodexTicket{}
	labelsByModel := map[string][]string{}
	for _, model := range codexTicketNodeModels(account, cfg) {
		inventory, selected := s.codexTicketNodeInventory(egress.account, model, cfg, now)
		slots, labels := codexTicketNodeSlotLabels(inventory)
		slotsByModel[model], labelsByModel[model] = slots, labels
		view := CodexTicketNodeModel{Model: model, Configured: codexTicketConfigGatesModel(cfg, model), Slots: []CodexTicketNodeSlot{}}
		for i, slot := range slots {
			view.Slots = append(view.Slots, codexTicketNodeSlotView(labels[i], slot, selected, egress, cfg, mode, now))
		}
		view.Pending = s.codexTicketNodePendingView(account.ID, model, slots, labels)
		result.Models = append(result.Models, view)
	}
	for _, conn := range s.codexTicketNodeConnections(account.ID) {
		result.Connections = append(result.Connections, codexTicketNodeConnectionView(conn, slotsByModel, labelsByModel))
	}
	return result, nil
}

func (s *OpenAIGatewayService) ProbeOpenAICodexTicketNodes(ctx context.Context, accountID int64, input CodexTicketNodeProbeInput) (CodexTicketNodeProbeResult, error) {
	started := time.Now()
	model, source := normalizeOpenAICodexTicketModel(input.Model), strings.TrimSpace(input.Source)
	count := input.Count
	if count == 0 {
		count = 1
	}
	result := CodexTicketNodeProbeResult{AccountID: accountID, Model: model, Source: source,
		NodeCounts: map[string]int{}, Rounds: []CodexTicketNodeProbeRound{}, StartedAt: started.UTC()}
	if s == nil || s.httpUpstream == nil {
		return result, ErrCodexTicketNodesUnavailable
	}
	account, err := s.codexTicketNodeAccount(ctx, accountID)
	if err != nil {
		return result, err
	}
	switch source {
	case CodexTicketNodeSourceTicket:
		result.Slot = strings.TrimSpace(input.Slot)
	case CodexTicketNodeSourceEmptyJar, CodexTicketNodeSourceStickyJar:
	default:
		return result, &CodexTicketNodeProbeError{Reason: "invalid_source"}
	}
	if count < 1 || count > codexTicketNodeProbeMaxRounds {
		return result, &CodexTicketNodeProbeError{Reason: "invalid_count"}
	}
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	known := false
	for _, candidate := range codexTicketNodeModels(account, cfg) {
		known = known || candidate == model
	}
	if model == "" || !known {
		return result, &CodexTicketNodeProbeError{Reason: "model_unavailable"}
	}
	// 只读凭据中的现有 token，不在这里触发刷新写库。
	token := strings.TrimSpace(account.GetCredential("access_token"))
	if token == "" {
		return result, &CodexTicketNodeProbeError{Reason: "token_unavailable"}
	}
	if _, busy := codexTicketNodeProbeInflight.LoadOrStore(accountID, struct{}{}); busy {
		return result, ErrCodexTicketNodeProbeBusy
	}
	defer codexTicketNodeProbeInflight.Delete(accountID)

	// 与质量检测相同，只跟随业务当前出口，不分配、换绑或解除代理冷却。
	egress := s.codexTicketNodeEgress(ctx, account)
	if !egress.available {
		return result, &CodexTicketNodeProbeError{Reason: "proxy_unavailable"}
	}
	result.Proxy, result.EgressCountry = egress.proxy, egress.country
	mode := s.codexCookieModeForRequest(ctx, account)
	result.CookieMode = mode

	var ticket *openAICodexTicket
	if source == CodexTicketNodeSourceTicket {
		inventory, selected := s.codexTicketNodeInventory(egress.account, model, cfg, time.Now())
		slots, labels := codexTicketNodeSlotLabels(inventory)
		for i, slot := range slots {
			if result.Slot == "" && sameCodexTicket(slot, selected) || result.Slot != "" && labels[i] == result.Slot {
				ticket, result.Slot = codexTicketLeaf(slot), labels[i]
				break
			}
		}
		if ticket == nil {
			reason := "no_usable_ticket"
			if result.Slot != "" {
				reason = "slot_not_found"
			}
			return result, &CodexTicketNodeProbeError{Reason: reason}
		}
		// 票据只能从采集它的出口重放，避免同一凭据从另一个 IP 出现。
		if ticket.Egress == "" || ticket.Egress != openAICodexTicketEgress(egress.proxyURL) {
			return result, &CodexTicketNodeProbeError{Reason: "egress_changed"}
		}
		// 与业务发送一致按当前 cookie_mode 投影。
		ticket.CookieMode = mode
		result.SlotNode = codexTicketNodeCookieNode(codexTicketNodeCookieViews(ticket.Cookies, mode), true)
	}

	deadline := started.Add(codexTicketNodeProbeBudget)
	stickySession := uuid.NewString()
	var stickyJar http.CookieJar
	if source == CodexTicketNodeSourceStickyJar {
		stickyJar = newOpenAICodexTicketCookieJar()
	}
	for i := 0; i < count; i++ {
		round := CodexTicketNodeProbeRound{Index: i + 1, NodeOutcome: "skipped", SentCookies: []CodexTicketNodeCookie{}, ReceivedCookies: []CodexTicketNodeCookie{}}
		remaining := time.Until(deadline)
		if ctx.Err() != nil || remaining < codexTicketNodeProbeMinRound {
			round.Status, round.Reason = "skipped", "budget_exhausted"
			if ctx.Err() != nil {
				round.Reason = "canceled"
			}
			result.Rounds = append(result.Rounds, round)
			continue
		}
		// empty_jar 每轮全新会话；sticky_jar 复用会话与 jar，观察负载均衡是否按回传 __oailb 粘住节点。
		session, jar := uuid.NewString(), http.CookieJar(nil)
		switch source {
		case CodexTicketNodeSourceTicket:
			if strings.TrimSpace(ticket.SessionID) != "" {
				session = ticket.SessionID
			}
		case CodexTicketNodeSourceStickyJar:
			session, jar = stickySession, stickyJar
		}
		s.runCodexTicketNodeProbeRound(ctx, &round, egress, token, model, session, ticket, jar, min(codexTicketNodeProbeRoundLimit, remaining))
		result.NodeCounts[codexTicketNodeCountKey(round.EffectiveNode)]++
		result.Rounds = append(result.Rounds, round)
	}
	result.DurationMs = time.Since(started).Milliseconds()
	return result, nil
}

// 单轮直接调用构造/发送，不经过 probeOpenAICodexTicket：后者会对报文 Cookie 打码并联动熔断。
// Config 为空时使用原始报文采集与默认 chatgpt.com 目标；BackgroundQuality 禁止连接失败触发代理冷却。
func (s *OpenAIGatewayService) runCodexTicketNodeProbeRound(ctx context.Context, round *CodexTicketNodeProbeRound, egress codexTicketNodeEgress, token, model, session string, ticket *openAICodexTicket, jar http.CookieJar, timeout time.Duration) {
	roundCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	startedAt := time.Now().UTC()
	round.StartedAt, round.SessionID = &startedAt, session
	responseID := ""
	in := openAICodexTicketProbeInput{Account: egress.account, Token: token, Model: model, ProxyURL: egress.proxyURL, SessionID: &session,
		CookieJar: jar, ResponseID: &responseID, SkipSchedulerAdmission: true, BackgroundQuality: true}
	req, err := s.buildOpenAICodexTicketProbeRequest(roundCtx, in)
	if err != nil {
		round.Status, round.Reason, round.NodeOutcome = "failed", codexTicketAttemptErrorReason("build", err), "not_sent"
		return
	}
	if ticket != nil {
		ticket.applyHeaders(req.Header)
	}
	capture := startCodexTicketExchange(in, req)
	sent := (&http.Request{Header: req.Header}).Cookies()
	start := time.Now()
	resp, err := s.doOpenAICodexTicketProbe(req, in)
	round.HeaderLatencyMs = time.Since(start).Milliseconds()
	capture.captureResponse(resp)
	var timer *codexTicketNodeBodyTimer
	var received []*http.Cookie
	if resp != nil {
		round.HTTPStatus = resp.StatusCode
		round.TurnStateLength = len(extractOpenAICodexTurnState(resp.Header))
		received = resp.Cookies()
		if resp.Body != nil {
			timer = &codexTicketNodeBodyTimer{ReadCloser: resp.Body, start: start}
			resp.Body = timer
		}
	}
	round.Status = "failed"
	switch {
	case err != nil:
		round.Reason = codexTicketAttemptErrorReason("probe", err)
	case resp == nil || resp.Body == nil:
		round.Reason = "probe_response_incomplete"
	case resp.StatusCode != http.StatusOK:
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
		if capture != nil {
			capture.exchange.UpstreamError = classifyCodexTicketUpstreamError(payload, resp.StatusCode, resp.Header.Get("Retry-After"))
		}
		round.Reason = "probe_http_rejected"
	default:
		if err := readOpenAICodexTicketProbeResponseWithDiagnostic(resp.Body, model, capture); err != nil {
			round.Reason = codexTicketAttemptErrorReason("probe", err)
		} else {
			round.Status = "ok"
		}
	}
	// 关闭后采集正文才写入 exchange。
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	round.TotalLatencyMs = time.Since(start).Milliseconds()
	if timer != nil {
		round.FirstByteMs, round.FirstDeltaMs = codexTicketNodeMillis(timer.firstByte), codexTicketNodeMillis(timer.firstDelta)
	}
	if jar != nil && resp != nil {
		storeOpenAICodexTicketCookies(jar, req, resp)
	}
	if capture != nil {
		round.Exchange = capture.exchange
	}
	round.ResponseID = strings.TrimSpace(responseID)
	round.SentCookies = codexTicketNodeCookieViews(sent, "")
	round.ReceivedCookies = codexTicketNodeCookieViews(received, "")
	round.SentNode, round.ReceivedNode, round.NodeOutcome = codexTicketNodeOutcome(round.SentCookies, round.ReceivedCookies, time.Now())
	if resp == nil {
		round.NodeOutcome = "no_response"
	}
	round.EffectiveNode = round.ReceivedNode
	if round.EffectiveNode == nil && round.NodeOutcome == "kept" {
		round.EffectiveNode = round.SentNode
	}
}

func (s *OpenAIGatewayService) codexTicketNodeAccount(ctx context.Context, accountID int64) (*Account, error) {
	if s == nil || s.accountRepo == nil || accountID <= 0 {
		return nil, ErrCodexTicketNodesUnavailable
	}
	account, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if account == nil || !account.IsOpenAIOAuthLike() || account.IsShadow() {
		return nil, ErrCodexTicketNodesUnavailable
	}
	return account, nil
}

// 在克隆账号上解析业务当前出口，读取结果不回写原账号。
func (s *OpenAIGatewayService) codexTicketNodeEgress(ctx context.Context, account *Account) codexTicketNodeEgress {
	egress := codexTicketNodeEgress{account: cloneOpenAICodexTicketAccount(account)}
	if !s.readCodexModelQualityProxy(ctx, egress.account) {
		return egress
	}
	egress.available = true
	egress.proxyURL = resolveAccountProxyURL(egress.account)
	var id int64
	var name string
	if egress.account.Proxy != nil {
		id, name = egress.account.Proxy.ID, egress.account.Proxy.Name
	}
	egress.proxy = codexTicketProxySnapshot(egress.proxyURL, id, name)
	egress.country = s.codexTicketProxyEgressCountry(ctx, id)
	return egress
}

// 与业务取票同一分片锁和合并逻辑，返回库存副本及业务当前会选中的叶票。
func (s *OpenAIGatewayService) codexTicketNodeInventory(account *Account, model string, cfg config.OpenAICodexTicketConfig, now time.Time) (*openAICodexTicket, *openAICodexTicket) {
	key := openAICodexTicketKey(account.ID, model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	inventory := s.availableCodexTicketInventory(key, s.codexTicketInventoryLocked(account, model))
	return inventory, selectOpenAICodexTicket(inventory, account, cfg, now)
}

// 配置模型在前，其后补上账号里仍残留票据的模型。
func codexTicketNodeModels(account *Account, cfg config.OpenAICodexTicketConfig) []string {
	seen, models, extra := map[string]bool{}, []string{}, []string{}
	for _, model := range cfg.Models {
		if model = normalizeOpenAICodexTicketModel(model); model != "" && !seen[model] {
			seen[model] = true
			models = append(models, model)
		}
	}
	for key := range account.Extra {
		if !IsOpenAICodexTicketExtraKey(key) {
			continue
		}
		if IsOpenAICodexTicketMetaExtraKey(key) {
			continue
		}
		if model := normalizeOpenAICodexTicketModel(strings.TrimPrefix(key, openAICodexTicketExtraKeyPrefix)); model != "" && !seen[model] {
			seen[model] = true
			extra = append(extra, model)
		}
	}
	sort.Strings(extra)
	return append(models, extra...)
}

// 顺序与 codexTicketSlots 一致。
func codexTicketNodeSlotLabels(inventory *openAICodexTicket) ([]*openAICodexTicket, []string) {
	if inventory == nil {
		return nil, nil
	}
	slots, labels := []*openAICodexTicket{inventory}, []string{"primary"}
	if inventory.Standby != nil {
		slots, labels = append(slots, inventory.Standby), append(labels, "standby")
	}
	n := 0
	for _, reserve := range inventory.Reserve {
		if reserve != nil {
			n++
			slots, labels = append(slots, reserve), append(labels, "reserve-"+strconv.Itoa(n))
		}
	}
	return slots, labels
}

func codexTicketNodeSlotView(label string, slot, selected *openAICodexTicket, egress codexTicketNodeEgress, cfg config.OpenAICodexTicketConfig, mode string, now time.Time) CodexTicketNodeSlot {
	view := CodexTicketNodeSlot{Label: label, BusinessSelected: sameCodexTicket(slot, selected), Usable: slot.usable(now, egress.account, cfg),
		Revoked: slot.Revoked, Verified: slot.Verified, VerificationSkipped: slot.VerificationSkipped, CredentialMode: slot.CredentialMode,
		SessionID: slot.SessionID, State: slot.State, StateLength: len(slot.State), HarvestProxyID: slot.HarvestProxyID,
		HarvestProxyName: slot.HarvestProxyName, HarvestCountry: slot.HarvestCountry,
		HarvestEgressDiffers: slot.HarvestEgress != "" && slot.HarvestEgress != slot.Egress,
		CapturedAt:           codexTicketNodeTime(slot.CapturedAt), OriginCapturedAt: codexTicketNodeTime(slot.OriginCapturedAt),
		ExpiresAt: codexTicketNodeTime(slot.ExpiresAt), HardExpiresAt: codexTicketNodeTime(slot.hardExpiresAt()),
		StateExpiresAt: codexTicketNodeTime(slot.StateExpiresAt), RevalidateAt: codexTicketNodeTime(slot.RevalidateAt),
		RevalidatedAt: codexTicketNodeTime(slot.RevalidatedAt), Attempts: slot.Attempts, Invalidation: slot.Invalidation}
	if egress.available && slot.Egress != "" {
		matches := slot.Egress == openAICodexTicketEgress(egress.proxyURL)
		view.EgressMatchesCurrent = &matches
	}
	view.Cookies = codexTicketNodeCookieViews(slot.Cookies, mode)
	view.RouteNode = codexTicketNodeCookieNode(view.Cookies, false)
	view.SentNode = codexTicketNodeCookieNode(view.Cookies, true)
	if node, ok := codexTicketRouteNode(slot); ok {
		view.CrossRegion = codexGatewayRouteCrossRegion(node, slot.HarvestCountry)
	}
	// 按当前 cookie_mode 投影后计算实际发送的路由指纹与到期时间。
	sent := codexTicketLeaf(slot)
	sent.CookieMode = mode
	view.RouteFingerprint = codexTicketRouteFingerprint(sent)
	view.RouteExpiresAt = codexTicketNodeTime(codexTicketRouteExpiresAt(sent))
	return view
}

func (s *OpenAIGatewayService) codexTicketNodePendingView(accountID int64, model string, slots []*openAICodexTicket, labels []string) *CodexTicketNodePending {
	pending := s.pendingCodexTicketCookies(accountID, model)
	if pending == nil {
		return nil
	}
	view := &CodexTicketNodePending{ExpiresAt: codexTicketNodeTime(pending.ExpiresAt), Cookies: []CodexTicketNodeCookie{}}
	if pending.Ticket != nil {
		for i, slot := range slots {
			if sameCodexTicket(slot, pending.Ticket) {
				view.TicketLabel = labels[i]
				break
			}
		}
		view.TicketNode = codexTicketNodeCookieNode(codexTicketNodeCookieViews(pending.Ticket.Cookies, ""), false)
	}
	if candidate := pending.Candidate; candidate != nil {
		view.HeaderChanged = candidate.HeaderChanged
		view.CandidateCapturedAt, view.CandidateExpiresAt = codexTicketNodeTime(candidate.CapturedAt), codexTicketNodeTime(candidate.ExpiresAt)
		view.Cookies = codexTicketNodeCookieViews(candidate.Cookies, "")
		view.CandidateNode = codexTicketNodeCookieNode(view.Cookies, false)
	}
	return view
}

// 池锁内只复制发布后不再变化的字段，解码放到锁外。
func (s *OpenAIGatewayService) codexTicketNodeConnections(accountID int64) []codexTicketNodeConnCopy {
	pool := s.getOpenAIWSConnPool()
	if pool == nil {
		return nil
	}
	ap, ok := pool.getAccountPool(accountID)
	if !ok || ap == nil {
		return nil
	}
	ap.mu.Lock()
	copies := make([]codexTicketNodeConnCopy, 0, len(ap.conns))
	for _, conn := range ap.conns {
		if conn == nil {
			continue
		}
		copies = append(copies, codexTicketNodeConnCopy{id: conn.id, affinity: conn.routingAffinity, fingerprint: conn.routeFingerprint,
			receipt: conn.codexTicketReceipt, headers: conn.handshakeHeaders, strict: conn.routeRequest != nil,
			leased: conn.isLeased(), leasedBefore: conn.leasedBefore.Load(), prewarmed: conn.isPrewarmed(),
			closed: conn.isClosed(), unusable: conn.isUnusable(), created: conn.createdAt(), lastUsed: conn.lastUsedAt()})
	}
	ap.mu.Unlock()
	sort.Slice(copies, func(i, j int) bool {
		if !copies[i].created.Equal(copies[j].created) {
			return copies[i].created.Before(copies[j].created)
		}
		return copies[i].id < copies[j].id
	})
	return copies
}

func codexTicketNodeConnectionView(conn codexTicketNodeConnCopy, slotsByModel map[string][]*openAICodexTicket, labelsByModel map[string][]string) CodexTicketNodeConnection {
	view := CodexTicketNodeConnection{ID: conn.id, RoutingAffinity: conn.affinity, RouteFingerprint: conn.fingerprint, Strict: conn.strict,
		CreatedAt: codexTicketNodeTime(conn.created), LastUsedAt: codexTicketNodeTime(conn.lastUsed), Leased: conn.leased,
		LeasedBefore: conn.leasedBefore, Prewarmed: conn.prewarmed, Closed: conn.closed, Unusable: conn.unusable,
		SentCookies: []CodexTicketNodeCookie{}}
	if strings.HasPrefix(conn.affinity, "model=") {
		view.Model, _, _ = strings.Cut(strings.TrimPrefix(conn.affinity, "model="), ";")
	}
	if conn.receipt != nil {
		ticket := &conn.receipt.ticket
		view.HasReceipt, view.Model = true, ticket.Model
		// 握手时发送的 Cookie 按 receipt 投影重建，与 observeHandshake 一致。
		view.SentCookies = codexTicketNodeCookieViews(filterCodexCookies(ticket.Cookies, ticket.CookieMode), "")
		view.SentNode = codexTicketNodeCookieNode(view.SentCookies, false)
		labels := labelsByModel[ticket.Model]
		for i, slot := range slotsByModel[ticket.Model] {
			if sameCodexTicket(slot, ticket) {
				view.SlotLabel = labels[i]
				break
			}
		}
	}
	view.HandshakeHeaders, view.HandshakeHeadersTruncated = codexTicketRawHeaders(conn.headers)
	view.ReceivedCookies = codexTicketNodeCookieViews((&http.Response{Header: conn.headers}).Cookies(), "")
	view.ReceivedNode = codexTicketNodeCookieNode(view.ReceivedCookies, false)
	return view
}

// mode 非空时标记会被当前 cookie_mode 过滤掉的 Cookie。
func codexTicketNodeCookieViews(cookies []*http.Cookie, mode string) []CodexTicketNodeCookie {
	views := make([]CodexTicketNodeCookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie != nil {
			views = append(views, codexTicketNodeCookieView(cookie, mode))
		}
	}
	return views
}

func codexTicketNodeCookieView(cookie *http.Cookie, mode string) CodexTicketNodeCookie {
	view := CodexTicketNodeCookie{Name: cookie.Name, Value: cookie.Value, Domain: cookie.Domain, Path: cookie.Path,
		ExpiresAt: codexTicketNodeTime(cookie.Expires), MaxAge: cookie.MaxAge, Secure: cookie.Secure, HttpOnly: cookie.HttpOnly,
		SameSite: codexTicketNodeSameSite(cookie.SameSite)}
	if mode != "" {
		view.ExcludedByMode = codexCookieExcluded(normalizeCodexCookieMode(mode), cookie.Name)
	}
	if cookie.Name != codexOAILBCookieName {
		return view
	}
	claims, expires, err := decodeCodexOAILBCookie(cookie.Value)
	if err != nil {
		view.DecodeError = err.Error()
		return view
	}
	view.Claims, view.ClaimExpiresAt = claims, codexTicketNodeTime(expires)
	host, _ := claims["host"].(string)
	view.Node = codexTicketNodeInfoFromHost(host)
	return view
}

// sentOnly 为 true 时跳过被当前 cookie_mode 过滤的 Cookie。
func codexTicketNodeCookieNode(cookies []CodexTicketNodeCookie, sentOnly bool) *CodexTicketNodeInfo {
	for _, cookie := range cookies {
		if cookie.Name == codexOAILBCookieName && cookie.Node != nil && !(sentOnly && cookie.ExcludedByMode) {
			return cookie.Node
		}
	}
	return nil
}

func codexTicketNodeInfoFromHost(host string) *CodexTicketNodeInfo {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil
	}
	info := &CodexTicketNodeInfo{Host: host}
	if node, ok := parseCodexGatewayNodeHost(host); ok {
		info.Recognized, info.Name, info.Number = true, node.Name(), node.Number
		info.Country, info.Region, info.MacroRegion = node.Country, node.Region, node.MacroRegion()
	}
	return info
}

// 比较本轮发送与响应下发的 __oailb，判断负载均衡是分配、保持、刷新还是改派节点。
func codexTicketNodeOutcome(sent, received []CodexTicketNodeCookie, now time.Time) (*CodexTicketNodeInfo, *CodexTicketNodeInfo, string) {
	var sentLB, receivedLB *CodexTicketNodeCookie
	for i := range sent {
		if sent[i].Name == codexOAILBCookieName && sentLB == nil {
			sentLB = &sent[i]
		}
	}
	for i := range received {
		if received[i].Name == codexOAILBCookieName {
			receivedLB = &received[i]
		}
	}
	var sentNode *CodexTicketNodeInfo
	if sentLB != nil {
		sentNode = sentLB.Node
	}
	switch {
	case sentLB == nil && receivedLB == nil:
		return nil, nil, "no_route"
	case receivedLB == nil:
		return sentNode, nil, "kept"
	case receivedLB.Value == "" || receivedLB.MaxAge < 0 || receivedLB.ExpiresAt != nil && !receivedLB.ExpiresAt.After(now):
		return sentNode, nil, "cleared"
	case receivedLB.Node == nil:
		return sentNode, nil, "unparsed"
	case sentLB == nil:
		return nil, receivedLB.Node, "assigned"
	case sentNode != nil && strings.EqualFold(sentNode.Host, receivedLB.Node.Host):
		return sentNode, receivedLB.Node, "refreshed"
	default:
		return sentNode, receivedLB.Node, "changed"
	}
}

func codexTicketNodeCountKey(node *CodexTicketNodeInfo) string {
	switch {
	case node == nil:
		return "unknown"
	case node.Name != "":
		return node.Name
	default:
		return node.Host
	}
}

func codexTicketNodeSameSite(mode http.SameSite) string {
	switch mode {
	case http.SameSiteDefaultMode:
		return "default"
	case http.SameSiteLaxMode:
		return "lax"
	case http.SameSiteStrictMode:
		return "strict"
	case http.SameSiteNoneMode:
		return "none"
	default:
		return ""
	}
}

func codexTicketNodeTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	value = value.UTC()
	return &value
}

func codexTicketNodeMillis(value time.Duration) *int64 {
	if value <= 0 {
		return nil
	}
	ms := value.Milliseconds()
	return &ms
}

// 记录响应首字节与首个 *.delta 事件的耗时，用于比较不同节点的首 token 延迟。
type codexTicketNodeBodyTimer struct {
	io.ReadCloser
	start                 time.Time
	firstByte, firstDelta time.Duration
	tail                  []byte
}

func (t *codexTicketNodeBodyTimer) Read(p []byte) (int, error) {
	n, err := t.ReadCloser.Read(p)
	if n <= 0 {
		return n, err
	}
	elapsed := time.Since(t.start)
	if elapsed <= 0 {
		elapsed = time.Nanosecond
	}
	if t.firstByte == 0 {
		t.firstByte = elapsed
	}
	if t.firstDelta == 0 {
		window := append(append([]byte(nil), t.tail...), p[:n]...)
		if bytes.Contains(window, []byte(`.delta"`)) {
			t.firstDelta = elapsed
		}
		keep := min(len(window), 32)
		t.tail = append([]byte(nil), window[len(window)-keep:]...)
	}
	return n, err
}
