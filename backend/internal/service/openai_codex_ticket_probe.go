package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const openAICodexTicketProbeResponseLimit = 4 << 20

var errOpenAICodexTicketControlsChanged = errors.New("codex ticket probe stopped by current controls")

type openAICodexTicketProbeInput struct {
	Account                       *Account
	Token, Model, ProxyURL, State string
	Timeout                       time.Duration
	CheckControls                 bool
	Attempt                       *CodexTicketAttempt
	Config                        *config.OpenAICodexTicketConfig
	SubscriptionTier              string
	HarvestProxyPolicy            *codexTicketProxyPolicy
	HeaderSources                 *[]CodexTicketHeaderSource
	SessionEpoch                  *string
	SessionID                     *string
	CookieJar                     http.CookieJar
	// CookieCandidate receives a cloned jar when the upstream response sends a
	// changed Cookie. The candidate is intentionally separate from CookieJar:
	// callers must business-verify it before replacing the published snapshot.
	CookieCandidate *openAICodexTicketCookieCandidate
	// ResponseID receives the completed response.id for optional request
	// strategies that validate an HTTP continuation before opening WS.
	ResponseID           *string
	BusinessVerification bool
	// BusinessCredentialSnapshot freezes the credentials for a multi-round
	// quality probe. It is never persisted and is replaced only between probe
	// attempts, so a response cannot silently rotate the cookie used by later
	// rounds.
	BusinessCredentialSnapshot *openAICodexTicket
	SkipSchedulerAdmission     bool
	QualityVerification        bool
	FreezeCredentials          bool
	BackgroundQuality          bool
	HarvestProxy               *openAICodexTicketProxy
	Revalidation               bool
}

func (s *OpenAIGatewayService) fireOpenAICodexTicketProbe(ctx context.Context, account *Account, token, model, proxyURL string, attemptTimeout time.Duration) (string, int, error) {
	return s.probeOpenAICodexTicket(ctx, openAICodexTicketProbeInput{Account: account, Token: token, Model: model, ProxyURL: proxyURL, Timeout: attemptTimeout})
}

func (s *OpenAIGatewayService) probeOpenAICodexTicket(ctx context.Context, in openAICodexTicketProbeInput) (string, int, error) {
	if s == nil || s.httpUpstream == nil || in.Account == nil || strings.TrimSpace(in.Token) == "" || strings.TrimSpace(in.Model) == "" {
		return "", 0, errors.New("codex ticket probe input unavailable")
	}
	attemptCtx, cancel := context.WithTimeout(ctx, in.Timeout)
	defer cancel()
	req, err := s.buildOpenAICodexTicketProbeRequest(attemptCtx, in)
	if err != nil {
		return "", 0, err
	}
	diagnostic := startCodexTicketExchange(in, req)
	redactOpenAICodexTicketCookies(diagnostic)
	sentCookies := codexTicketBusinessCookieSnapshotForProbe(in)
	resp, err := s.doOpenAICodexTicketProbe(req, in)
	var originalHeaders http.Header
	if resp != nil {
		originalHeaders = resp.Header.Clone()
	}
	// 初次采集没有已发布快照，可以把响应 Cookie 放入采集 jar；业务复验
	// 必须保留原 jar，并把变化写入候选 jar，等待下一次复验后再发布。
	cookieChanged := false
	if in.FreezeCredentials {
		// 过滤投影的响应仍进入原票候选队列，不改变本次验证所用凭据。
		if sentCookies != nil && in.Config != nil && normalizeCodexCookieMode(sentCookies.CookieMode) != CodexCookiePreserve {
			s.captureCodexTicketCookieCandidate(&openAICodexTicketReceipt{
				account: in.Account, ticket: *sentCookies, config: *in.Config,
			}, req, resp)
		}
	} else if sentCookies == nil {
		storeOpenAICodexTicketCookies(in.CookieJar, req, resp)
	} else if candidate, changed := candidateOpenAICodexTicketCookies(in.CookieJar, req, resp,
		probeCookieTTL(in)); changed && in.CookieCandidate != nil {
		cookieChanged = true
		*in.CookieCandidate = *candidate
	}
	diagnostic.captureResponse(resp)
	redactOpenAICodexTicketCookies(diagnostic)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		return "", 0, err
	}
	if resp == nil {
		return "", 0, &codexTicketProbeResponseError{reason: "response_incomplete"}
	}
	if resp.StatusCode != http.StatusOK {
		// 非 200 也采集有界错误正文，便于区分鉴权、限流和代理拒绝。
		var payload []byte
		if resp.Body != nil {
			payload, _ = io.ReadAll(io.LimitReader(resp.Body, (64<<10)+1))
		}
		detail := classifyCodexTicketUpstreamError(payload, resp.StatusCode, resp.Header.Get("Retry-After"))
		if diagnostic != nil {
			diagnostic.exchange.UpstreamError = detail
		}
		rejected := &openAICodexTicketProbeRejected{Status: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After")}
		return "", resp.StatusCode, &codexTicketProbeClassifiedError{err: rejected, detail: detail}
	}
	if resp.Body == nil {
		return "", resp.StatusCode, &codexTicketProbeResponseError{reason: "response_incomplete"}
	}
	if err := readOpenAICodexTicketProbeResponseWithDiagnostic(resp.Body, in.Model, diagnostic); err != nil {
		return "", resp.StatusCode, err
	}
	if !in.FreezeCredentials && sentCookies != nil && sentCookies.cookieResponseChanged(originalHeaders) && !cookieChanged {
		return "", resp.StatusCode, &codexTicketProbeResponseError{reason: "ticket_rejected"}
	}
	if in.ResponseID != nil {
		*in.ResponseID = strings.TrimSpace(diagnosticResponseID(diagnostic))
	}
	return extractOpenAICodexTurnState(originalHeaders), resp.StatusCode, nil
}

func diagnosticResponseID(capture *codexTicketExchangeCapture) string {
	if capture == nil || capture.responseID == nil {
		return ""
	}
	return strings.TrimSpace(*capture.responseID)
}

func (s *OpenAIGatewayService) buildOpenAICodexTicketProbeRequest(ctx context.Context, in openAICodexTicketProbeInput) (*http.Request, error) {
	sessionID, err := s.openAICodexTicketProbeSessionID(ctx, in)
	if err != nil {
		return nil, err
	}
	if in.SessionID != nil {
		*in.SessionID = sessionID
	}
	body := []byte(`{"model":` + jsonString(in.Model) + `,"store":false,"stream":true,"instructions":"Reply with exactly: pong","input":[{"role":"user","content":[{"type":"input_text","text":"ping"}]}]}`)
	ctx = WithHTTPUpstreamProfile(ctx, HTTPUpstreamProfileOpenAIHarvest)
	ctx = WithHTTPUpstreamRedirectsDisabled(ctx)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexURL, bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("codex ticket probe request construction failed")
	}
	req.Close, req.Host = true, "chatgpt.com"
	req.Header.Set("Authorization", "Bearer "+in.Token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("session_id", sessionID)
	recordCodexTicketHeaderSources(in.HeaderSources, nil, req.Header, "probe_defaults", "Synthetic probe defaults; preview uses placeholder authorization")
	before := codexTicketPreviewHeaderSnapshot(in, req.Header)
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(ctx, s.accountRepo, req.Header, in.Account); err != nil {
		return nil, errors.New("codex ticket probe identity unavailable")
	}
	recordCodexTicketHeaderSources(in.HeaderSources, before, req.Header, "account_identity", "Account identity and FedRAMP policy; private ID replaced in preview")
	before = codexTicketPreviewHeaderSnapshot(in, req.Header)
	s.applyOpenAICodexTicketHarvestIdentity(req.Header, in.Account, in.Model)
	recordCodexTicketHeaderSources(in.HeaderSources, before, req.Header, "codex_identity", "Shared Codex identity policy and account User-Agent override")
	before = codexTicketPreviewHeaderSnapshot(in, req.Header)
	// 与业务请求保持相同的模型路由和能力声明，身份构造后清理旧实验头。
	stripOpenAILegacyResponsesBeta(req.Header)
	applyOpenAICodexBetaFeatures(nil, in.Account, req.Header)
	recordCodexTicketHeaderSources(in.HeaderSources, before, req.Header, "capability_policy", "Remove legacy experiment and declare current Codex capabilities")
	before = codexTicketPreviewHeaderSnapshot(in, req.Header)
	setOpenAICodexRoutingHintFromBody(req.Header, in.Account, body)
	recordCodexTicketHeaderSources(in.HeaderSources, before, req.Header, "routing_policy", "Routing hint derived from the final synthetic request body")
	if in.State != "" {
		req.Header.Set(openAICodexTurnStateHeader, in.State)
	}
	if cookies := codexTicketBusinessCookieSnapshotForProbe(in); cookies != nil {
		if !cookies.cookieUsable(time.Now(), *in.Config) {
			return nil, &codexTicketProbeResponseError{reason: "ticket_rejected"}
		}
		cookies.applyHeaders(req.Header)
	} else {
		applyOpenAICodexTicketCookies(in.CookieJar, req)
	}
	return req, nil
}

// 合成探测与业务复验均走现有专用传输；保留 TLS 配置，不经过插件路由。
func (s *OpenAIGatewayService) doOpenAICodexTicketProbe(req *http.Request, in openAICodexTicketProbeInput) (*http.Response, error) {
	profile, err := resolveMode1TLSProfile(in.Account)
	if err != nil {
		return nil, errors.New("codex ticket probe TLS profile unavailable")
	}
	if req.Context().Err() != nil || in.CheckControls && (!s.openAICodexTicketProbeAllowed(req.Context(), in.Account, in.Token) || !s.openAICodexTicketProbeConfigCurrent(req.Context(), in) || !s.codexTicketProxyPolicyCurrent(req.Context(), in)) {
		return nil, errOpenAICodexTicketControlsChanged
	}
	if err := s.admitCodexTicketProbe(req.Context(), in); err != nil {
		return nil, err
	} else if schedule := codexTicketScheduleFrom(req.Context()); schedule != nil {
		if err := schedule.scheduler.ValidateCodexTicket(req.Context(), schedule.reservation); err != nil {
			return nil, errOpenAICodexTicketControlsChanged
		}
	}
	// 等待代理或调度资源期间账号可能已被停用，发送前读取最新状态。
	if (in.CheckControls || in.FreezeCredentials) && !s.codexTicketAccountCurrentBeforePublish(req.Context(), in) {
		return nil, errOpenAICodexTicketControlsChanged
	}
	if (in.CheckControls || in.FreezeCredentials) && s.codexModelQualityCircuitPaused(req.Context(), in.Account, in.Model) {
		return nil, errOpenAICodexTicketControlsChanged
	}
	if in.Attempt != nil {
		if in.Attempt.StartedAt.IsZero() {
			in.Attempt.StartedAt = time.Now()
		}
		if codexTicketProbeBusiness(in) {
			var id int64
			var name string
			if in.QualityVerification && in.HarvestProxy != nil {
				id, name = in.HarvestProxy.proxyID, in.HarvestProxy.proxyName
			} else if in.Account.Proxy != nil {
				id, name = in.Account.Proxy.ID, in.Account.Proxy.Name
			}
			in.Attempt.BusinessProxy = codexTicketProxySnapshot(in.ProxyURL, id, name)
		}
	}
	var resp *http.Response
	if profile != nil && (s.cfg == nil || s.cfg.Gateway.TLSFingerprint.Enabled) {
		resp, err = s.httpUpstream.DoWithTLS(req, in.ProxyURL, in.Account.ID, in.Account.Mode1EffectiveConcurrency(), profile)
	} else {
		resp, err = s.httpUpstream.Do(req, in.ProxyURL, in.Account.ID, in.Account.Mode1EffectiveConcurrency())
	}
	recordCodexTicketProbeResponse(in, resp)
	recordCodexTicketProtectionResponse(in, resp)
	if err != nil {
		s.reportCodexProbeConnectionFailure(req.Context(), in, err)
		return resp, &codexTicketTransportError{&openAICodexTicketProbeIOError{cause: err}}
	}
	return resp, nil
}

func readOpenAICodexTicketProbeResponse(body io.Reader, model string) error {
	return readOpenAICodexTicketProbeResponseWithDiagnostic(body, model, nil)
}

func readOpenAICodexTicketProbeResponseWithDiagnostic(body io.Reader, model string, diagnostic *codexTicketExchangeCapture) (result error) {
	observer := newOpenAICodexTicketResponseObserver(model)
	observer.diagnostics = &codexTicketResponseDiagnostics{wireStatus: http.StatusOK}
	if diagnostic != nil {
		observer.diagnostics.signals = diagnostic.exchange.Signals
	}
	defer func() {
		diagnostic.captureObserver(observer)
		if result != nil && observer.diagnostics.upstreamError != nil {
			result = &codexTicketProbeClassifiedError{err: result, detail: observer.diagnostics.upstreamError}
		}
	}()
	reader := io.LimitReader(body, openAICodexTicketProbeResponseLimit+1)
	buffer := make([]byte, 16<<10)
	total, emptyReads := 0, 0
	for {
		n, err := reader.Read(buffer)
		total += n
		if total > openAICodexTicketProbeResponseLimit {
			observer.diagnostics.declaration.Truncated = true
			return &codexTicketProbeResponseError{reason: "response_incomplete"}
		}
		observer.Observe(buffer[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			observer.diagnostics.declaration.Truncated = true
			return &codexTicketTransportError{&openAICodexTicketProbeIOError{cause: err}}
		}
		if n > 0 {
			emptyReads = 0
		} else {
			emptyReads++
			if emptyReads >= 100 {
				return &codexTicketTransportError{&openAICodexTicketProbeIOError{cause: io.ErrNoProgress}}
			}
		}
	}
	observer.Finish()
	return codexTicketProbeResponseResult(observer)
}

// 原始传输错误仅保留在错误链中，以固定文本防止代理 URL 或凭据进入日志。
type openAICodexTicketProbeIOError struct{ cause error }

func (e *openAICodexTicketProbeIOError) Error() string {
	message := strings.ToLower(e.cause.Error())
	for _, signal := range []string{"connection reset", "broken pipe", "tls handshake timeout", "proxyconnect tcp"} {
		if strings.Contains(message, signal) {
			return "codex ticket probe transport failed: " + signal
		}
	}
	return "codex ticket probe transport failed"
}

func (e *openAICodexTicketProbeIOError) Unwrap() error { return e.cause }
