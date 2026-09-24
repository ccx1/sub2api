package service

import (
	"context"
	"crypto/sha256"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	openaiwsv2 "github.com/Wei-Shaw/sub2api/internal/service/openai_ws_v2"
	coderws "github.com/coder/websocket"
	"github.com/tidwall/gjson"
)

// 只捕获请求握手实际携带的原生票据，不能从响应头或后续缓存反推。
type openAICodexTicketWSReceipt struct {
	account *Account
	ticket  openAICodexTicket
	config  config.OpenAICodexTicketConfig
	service *OpenAIGatewayService
	once    sync.Once
}

func codexTicketWSReceiptFromSnapshot(receipt *openAICodexTicketReceipt) *openAICodexTicketWSReceipt {
	if receipt == nil {
		return nil
	}
	return &openAICodexTicketWSReceipt{account: receipt.account, ticket: receipt.ticket, config: receipt.config, service: receipt.service}
}

func (r *openAICodexTicketWSReceipt) invalidate(ctx context.Context, s *OpenAIGatewayService, detail *CodexTicketInvalidation) {
	if r != nil {
		r.once.Do(func() { go s.invalidateOpenAICodexTicket(ctx, r.account, &r.ticket, detail) })
	}
}

func (r *openAICodexTicketWSReceipt) observeHandshake(ctx context.Context, s *OpenAIGatewayService, headers http.Header) {
	if r == nil {
		return
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, chatgptCodexURL, nil)
	r.ticket.applyHeaders(req.Header)
	// 握手已发送，观察响应时不能按当前时间过滤它实际携带的旧快照。
	if r.ticket.usesCookies() {
		req.Header.Del("Cookie")
		for _, cookie := range filterCodexCookies(r.ticket.Cookies, r.ticket.CookieMode) {
			if cookie != nil {
				req.AddCookie(cookie)
			}
		}
	}
	s.captureCodexTicketCookieCandidate(&openAICodexTicketReceipt{account: r.account, ticket: r.ticket, config: r.config}, req, &http.Response{Header: headers})
	// Handshake Set-Cookie is learned by the next scheduled revalidation. The
	// current WebSocket keeps its immutable receipt and is never revoked in
	// place by a cookie refresh.
	state := extractOpenAICodexTurnState(headers)
	if !r.ticket.usesCookies() && codexTicketStateRejected(state, config.NormalizeOpenAICodexTicketConfig(r.config)) {
		detail := codexTicketRejectedInvalidation("websocket_handshake", state)
		detail.Signals = codexTicketSignalsFromHeaders(headers)
		r.invalidate(ctx, s, detail)
	}
}

func (r *openAICodexTicketWSReceipt) watch(ctx context.Context, s *OpenAIGatewayService, model string) *openAICodexTicketWSWatchdog {
	if r == nil || model != r.ticket.Model {
		return nil
	}
	watchdog := &openAICodexTicketWSWatchdog{observer: newOpenAICodexTicketResponseObserver(model)}
	watchdog.quality = func() { s.markCodexModelQualityAnomaly(r.account.ID, r.ticket.Model) }
	watchdog.observer.diagnostics = &codexTicketResponseDiagnostics{wireStatus: http.StatusSwitchingProtocols}
	watchdog.invalidate = func() {
		r.invalidate(ctx, s, watchdog.invalidation("websocket"))
	}
	return watchdog
}

type openAICodexTicketWSWatchdog struct {
	observer    *openAICodexTicketResponseObserver
	invalidate  func()
	quality     func()
	qualityOnce sync.Once
}

func (w *openAICodexTicketWSWatchdog) invalidation(source string) *CodexTicketInvalidation {
	detail := newCodexTicketInvalidation("response_model_mismatch", source, w.observer.reportedModels)
	if w.observer.diagnostics != nil {
		detail.Signals = cloneCodexTicketSignals(w.observer.diagnostics.signals)
	}
	return detail
}

func (w *openAICodexTicketWSWatchdog) observe(payload []byte) {
	if w == nil || len(payload) > openAICodexTicketObservedFrameLimit {
		return
	}
	// WS 消息已有完整 JSON 边界，复用事件判定且不改写传输字节。
	w.observer.inspect(payload)
	completed, matches := w.observer.Result()
	if w.quality != nil && (w.observer.failed || w.observer.protocolCompleted && (!completed || !matches)) {
		w.qualityOnce.Do(w.quality)
	}
	if completed && !matches {
		w.invalidate()
	}
}

// 使用连接实际发送的快照，复用时不能用本轮准备的请求头反推握手票据。
func confirmedOpenAICodexTicketWSReceipt(_ *openAICodexTicketWSReceipt, lease *openAIWSConnLease) *openAICodexTicketWSReceipt {
	if lease == nil || lease.conn == nil {
		return nil
	}
	return lease.conn.codexTicketReceipt
}

type openAICodexTicketWSIdentity struct {
	state      [32]byte
	model      string
	capturedAt int64
	cookieMode string
}

func (r *openAICodexTicketWSReceipt) identity() openAICodexTicketWSIdentity {
	if r == nil {
		return openAICodexTicketWSIdentity{}
	}
	return openAICodexTicketWSIdentity{state: sha256.Sum256([]byte(r.ticket.credentialIdentity())),
		model: r.ticket.Model, capturedAt: r.ticket.CapturedAt.UnixNano(), cookieMode: normalizeCodexCookieMode(r.ticket.CookieMode)}
}

func codexTicketWSCookieTransition(current, next *openAICodexTicketWSReceipt) bool {
	if current != nil && current.config.FailClosed || next != nil && next.config.FailClosed {
		return false
	}
	if normalizeCodexCookieMode(current.identity().cookieMode) != normalizeCodexCookieMode(next.identity().cookieMode) {
		return false
	}
	return current != nil && current.ticket.usesCookies() || next != nil && next.ticket.usesCookies()
}

func (r *openAICodexTicketWSReceipt) validate(now time.Time) error {
	if r == nil || !r.config.FailClosed && normalizeCodexCookieMode(r.ticket.CookieMode) == CodexCookiePreserve {
		return nil
	}
	if !r.ticket.usable(now, r.account, r.config) {
		return ErrOpenAICodexTicketUnavailable
	}
	if s := r.service; s != nil {
		current := s.lookupOpenAICodexTicketForConfig(r.account, r.ticket.Model, r.config)
		if !current.usable(now, r.account, r.config) || s.codexTicketRevoked(openAICodexTicketKey(r.account.ID, r.ticket.Model), &r.ticket) {
			return ErrOpenAICodexTicketUnavailable
		}
	}
	return nil
}

func discardExpiredOpenAICodexTicketWSHandshake(req *openAIWSAcquireRequest, now time.Time) {
	receipt := req.CodexTicketReceipt
	if receipt == nil || !receipt.ticket.usesCookies() || receipt.config.FailClosed ||
		normalizeCodexCookieMode(receipt.ticket.CookieMode) != CodexCookiePreserve {
		return
	}
	expired := !receipt.ticket.effectiveExpiresAt(receipt.config).After(now)
	for _, cookie := range receipt.ticket.Cookies {
		if cookie == nil || !cookie.Expires.After(now) {
			expired = true
		}
	}
	if expired {
		// 排队和身份头生成可能耗尽短 TTL；只清本次新握手，不改已有连接。
		receipt.ticket.clearHeaders(req.Headers)
		req.CodexTicketReceipt = nil
	}
}

func validOpenAICodexTicketWSReceiptAccount(req openAIWSAcquireRequest) bool {
	receipt := req.CodexTicketReceipt
	return receipt == nil || req.Account != nil && receipt.account != nil &&
		receipt.account.ID == req.Account.ID && receipt.ticket.AccountID == req.Account.ID
}

func normalizeOpenAIWSTicketCompatibility(req openAIWSAcquireRequest) openAIWSHandshakeCompatibilityKey {
	key := normalizeOpenAIWSTransportCompatibility(req)
	key.cookieMode = openAIWSCookieMode(req)
	if normalizeOpenAIWSRouteAffinityMode(req.RouteAffinityMode) == CodexRouteAffinityStrict {
		key.strictRoute = openAIWSRequestRouteFingerprint(req.Headers)
		key.strictRoutingHint = normalizeOpenAIWSRoutingAffinity(req.Headers)
	}
	// 没有原生注入凭证时，不把客户端请求头当作受管理票据。
	if receipt := req.CodexTicketReceipt; receipt != nil && receipt.ticket.matchesHeaders(req.Headers) {
		key.codexTicket = receipt.identity()
	}
	return key
}

func openAIWSCookieMode(req openAIWSAcquireRequest) string {
	if req.CodexTicketReceipt != nil {
		return normalizeCodexCookieMode(req.CodexTicketReceipt.ticket.CookieMode)
	}
	return normalizeCodexCookieMode(req.CookieMode)
}

// 只约束新握手；已建立连接继续使用它实际发送的不可变凭据快照。
func validateOpenAIWSCookiePolicyDial(ctx context.Context, req openAIWSAcquireRequest) error {
	if req.CookiePolicyScope != "" {
		ctx = withCodexRequestStrategyConnectionScope(ctx, req.CookiePolicyScope)
	}
	var current string
	if req.CookiePolicyCurrent != nil {
		current = req.CookiePolicyCurrent(ctx)
	} else if receipt := req.CodexTicketReceipt; receipt != nil && receipt.service != nil {
		current = receipt.service.codexCookieModeForRequest(ctx, req.Account)
	} else {
		return nil
	}
	if normalizeCodexCookieMode(current) != openAIWSCookieMode(req) {
		return ErrOpenAICodexTicketUnavailable
	}
	if receipt := req.CodexTicketReceipt; receipt != nil && receipt.service != nil && receipt.ticket.usesCookies() {
		cfg := receipt.service.openAICodexTicketConfigForAccount(ctx, req.Account)
		if err := receipt.service.validateCodexCookieProjectionProof(ctx, req.Account, &receipt.ticket, cfg); err != nil {
			return err
		}
	}
	return nil
}

func (s *OpenAIGatewayService) refreshOpenAICodexTicketWSHeaders(ctx context.Context, account *Account, model string, req *openAIWSAcquireRequest) error {
	if req.CookiePolicyScope != "" {
		ctx = withCodexRequestStrategyConnectionScope(ctx, req.CookiePolicyScope)
	} else {
		req.CookiePolicyScope = codexRequestStrategyConnectionScope(ctx)
	}
	req.Headers = cloneHeader(req.Headers)
	if req.Headers == nil {
		req.Headers = make(http.Header)
	}
	if old := req.CodexTicketReceipt; old != nil {
		old.ticket.clearHeaders(req.Headers)
	}
	account, err := s.refreshOpenAICodexTicketWSAccount(ctx, account)
	if err != nil {
		return err
	}
	receipt, err := s.applyOpenAICodexTicketSnapshot(ctx, account, model, req.Headers)
	if err != nil {
		return err
	}
	req.CodexTicketReceipt = codexTicketWSReceiptFromSnapshot(receipt)
	return nil
}

// refreshOpenAICodexTicketWSHeadersForTurn keeps the snapshot captured while
// constructing the initial handshake. A first-attempt refresh could observe a
// different policy or cached ticket after the header was injected, leaving the
// dial request and its receipt out of sync. Later turns and explicit retries
// are allowed to refresh both together.
func (s *OpenAIGatewayService) refreshOpenAICodexTicketWSHeadersForTurn(
	ctx context.Context,
	account *Account,
	model string,
	turn int,
	retry int,
	req *openAIWSAcquireRequest,
) error {
	if turn <= 1 && retry == 0 {
		return nil
	}
	return s.refreshOpenAICodexTicketWSHeaders(ctx, account, model, req)
}

func (s *OpenAIGatewayService) refreshOpenAICodexTicketWSAccount(ctx context.Context, account *Account) (*Account, error) {
	if s == nil || !isOpenAICodexTicketAccount(account) {
		return account, nil
	}
	repo, ok := s.accountRepo.(codexTicketAccountReloader)
	if !ok {
		return account, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	latest, err := repo.GetCodexTicketAccountSnapshot(ctx, account.ID)
	if err != nil || latest == nil || latest.ID != account.ID || latest.Status != StatusActive ||
		OpenAICodexTicketAccountEnabled(latest) != OpenAICodexTicketAccountEnabled(account) ||
		openAICodexTicketAccountBinding(latest.ConfiguredProxySnapshot()) != openAICodexTicketAccountBinding(account.ConfiguredProxySnapshot()) {
		return nil, ErrOpenAICodexTicketUnavailable
	}
	latest = s.codexTicketAdmissionAccount(ctx, latest)
	if latest == nil || openAICodexTicketAccountBinding(latest) != openAICodexTicketAccountBinding(account) {
		// 只刷新票据快照不能改变既有 WS 出口；配置变化必须让客户端重新连接。
		return nil, ErrOpenAICodexTicketUnavailable
	}
	return latest, nil
}

type openAICodexTicketWSFrameConn struct {
	openaiwsv2.FrameConn
	mu                  sync.Mutex
	ctx                 context.Context
	service             *OpenAIGatewayService
	account             *Account
	cookiePolicyScope   string
	receipt             *openAICodexTicketWSReceipt
	watchdog            *openAICodexTicketWSWatchdog
	inFlight            bool
	observationDisabled bool
}

func (s *OpenAIGatewayService) observeOpenAICodexTicketWSFrames(ctx context.Context, conn openaiwsv2.FrameConn, receipt *openAICodexTicketWSReceipt, accounts ...*Account) openaiwsv2.FrameConn {
	var account *Account
	if receipt != nil {
		account = receipt.account
	} else if len(accounts) > 0 && OpenAICodexTicketAccountEnabled(accounts[0]) {
		account = cloneOpenAICodexTicketAccount(accounts[0])
	}
	if account == nil {
		return conn
	}
	return &openAICodexTicketWSFrameConn{FrameConn: conn, ctx: ctx, service: s, account: account, receipt: receipt,
		cookiePolicyScope: codexRequestStrategyConnectionScope(ctx)}
}

func (c *openAICodexTicketWSFrameConn) WriteFrame(ctx context.Context, kind coderws.MessageType, payload []byte) error {
	if (kind == coderws.MessageText || kind == coderws.MessageBinary) && gjson.GetBytes(payload, "type").String() == "response.create" {
		c.mu.Lock()
		model := gjson.GetBytes(payload, "model").String()
		req := openAIWSAcquireRequest{CookiePolicyScope: c.cookiePolicyScope}
		err := c.service.refreshOpenAICodexTicketWSHeaders(ctx, c.account, model, &req)
		changed := c.receipt.identity() != req.CodexTicketReceipt.identity()
		cookieMode := codexTicketWSCookieTransition(c.receipt, req.CodexTicketReceipt)
		if err != nil || changed && !cookieMode || c.receipt.validate(time.Now()) != nil {
			c.mu.Unlock()
			return NewOpenAIWSClientCloseError(coderws.StatusPolicyViolation,
				"upstream continuation connection is unavailable; please restart the conversation", ErrOpenAICodexTicketUnavailable)
		}
		if changed || c.inFlight {
			// 显式允许无票放行时保留 Cookie 会话；新凭据只在下次握手注入。
			// 无法关联并行响应时停止观察，绝不让后一请求覆盖前一请求快照。
			c.watchdog, c.observationDisabled = nil, true
		} else if !c.observationDisabled {
			c.watchdog = c.receipt.watch(c.ctx, c.service, model)
		}
		c.inFlight = true
		c.mu.Unlock()
	}
	return c.FrameConn.WriteFrame(ctx, kind, payload)
}

func (c *openAICodexTicketWSFrameConn) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	kind, payload, err := c.FrameConn.ReadFrame(ctx)
	if err == nil && (kind == coderws.MessageText || kind == coderws.MessageBinary) {
		c.mu.Lock()
		c.watchdog.observe(payload)
		if openAIWSPassthroughIsTerminalOutput(payload) {
			c.watchdog, c.inFlight = nil, false
		}
		c.mu.Unlock()
	}
	return kind, payload, err
}
