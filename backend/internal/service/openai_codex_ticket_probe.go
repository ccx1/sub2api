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
	"github.com/google/uuid"
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
	resp, err := s.doOpenAICodexTicketProbe(req, in)
	diagnostic.captureResponse(resp)
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
		if resp.Body != nil && diagnostic != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, (64<<10)+1))
		}
		return "", resp.StatusCode, &openAICodexTicketProbeRejected{Status: resp.StatusCode, RetryAfter: resp.Header.Get("Retry-After")}
	}
	if resp.Body == nil {
		return "", resp.StatusCode, &codexTicketProbeResponseError{reason: "response_incomplete"}
	}
	if err := readOpenAICodexTicketProbeResponseWithDiagnostic(resp.Body, in.Model, diagnostic); err != nil {
		return "", resp.StatusCode, err
	}
	return extractOpenAICodexTurnState(resp.Header), resp.StatusCode, nil
}

func (s *OpenAIGatewayService) buildOpenAICodexTicketProbeRequest(ctx context.Context, in openAICodexTicketProbeInput) (*http.Request, error) {
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
	req.Header.Set("OpenAI-Beta", "responses=experimental")
	req.Header.Set("session_id", uuid.NewString())
	if err := resolveAndSetOpenAIChatGPTAccountHeaders(ctx, s.accountRepo, req.Header, in.Account); err != nil {
		return nil, errors.New("codex ticket probe identity unavailable")
	}
	applyOpenAICodexTicketHarvestIdentity(req.Header, in.Model)
	if in.State != "" {
		req.Header.Set(openAICodexTurnStateHeader, in.State)
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
	if in.Attempt != nil {
		if in.Attempt.StartedAt.IsZero() {
			in.Attempt.StartedAt = time.Now()
		}
		if in.State != "" {
			var id int64
			var name string
			if in.Account.Proxy != nil {
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
	if err != nil {
		return resp, &codexTicketTransportError{&openAICodexTicketProbeIOError{cause: err}}
	}
	return resp, nil
}

func readOpenAICodexTicketProbeResponse(body io.Reader, model string) error {
	return readOpenAICodexTicketProbeResponseWithDiagnostic(body, model, nil)
}

func readOpenAICodexTicketProbeResponseWithDiagnostic(body io.Reader, model string, diagnostic *codexTicketExchangeCapture) error {
	observer := newOpenAICodexTicketResponseObserver(model)
	defer func() { diagnostic.setReportedModels(observer.reportedModels, observer.modelsTruncated) }()
	reader := io.LimitReader(body, openAICodexTicketProbeResponseLimit+1)
	buffer := make([]byte, 16<<10)
	total, emptyReads := 0, 0
	for {
		n, err := reader.Read(buffer)
		total += n
		if total > openAICodexTicketProbeResponseLimit {
			return &codexTicketProbeResponseError{reason: "response_incomplete"}
		}
		observer.Observe(buffer[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
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
