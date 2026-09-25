package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
)

func (s *OpenAIGatewayService) SetPluginManager(manager *PluginManager) {
	s.pluginManager = manager
}

// doOpenAIUpstream 只在 OpenAI OAuth 能力绑定已启用时把真实请求交给插件。
// 插件返回标准 http.Response，响应解析、错误映射、SSE 和计费仍由现有核心链处理。
func (s *OpenAIGatewayService) doOpenAIUpstream(request *http.Request, proxyURL string, account *Account) (response *http.Response, err error) {
	ctx, release, err := s.beginCodexTicketBusinessHold(request.Context(), account)
	if err != nil {
		return nil, err
	}
	request = request.WithContext(ctx)
	defer func() { attachCodexTicketBusinessHold(response, err, release) }()
	if err := s.applyOpenAIRequestTimezone(request, account); err != nil {
		return nil, err
	}
	scope := codexRequestStrategyConnectionScope(request.Context())
	if scope == "" {
		scope = CodexRequestStrategyScopeDedicated
	}
	if policy, enabled := s.codexRequestStrategyPolicyForScope(request.Context(), scope); enabled {
		if err := ApplyCodexRequestHeaderPolicy(request.Header, policy, account); err != nil {
			return nil, err
		}
	}
	if err := s.validateOpenAICodexTicketSend(request, account); err != nil {
		return nil, err
	}
	pluginHandled := false
	request = request.WithContext(context.WithValue(request.Context(), openAIPluginHandledKey{}, &pluginHandled))
	defer func() {
		if !pluginHandled {
			s.observeOpenAICodexTicketResponse(request, response)
		}
	}()
	if _, err := resolveMode1TLSProfile(account); err != nil {
		return nil, err
	}
	defer func() {
		if observeRandomProxyHTTPResult(request, account, s.accountRepo, response, err) {
			err = &randomProxyReportedTransportError{err}
		}
	}()
	// 每次出口尝试内部各自决定插件/原生传输与 TLS 指纹；票据观察与随机代理结果
	// 统计包在最终响应外层，只执行一次。
	return s.doUpstreamWithProxyFallback(request.Context(), request, account, proxyURL)
}

// openAIPluginHandledKey 让出口尝试回报本次响应是否由插件发送：插件自带凭据链路，
// 不能按原生票据快照观察其响应。
type openAIPluginHandledKey struct{}

func markOpenAIPluginHandled(ctx context.Context) {
	if handled, ok := ctx.Value(openAIPluginHandledKey{}).(*bool); ok && handled != nil {
		*handled = true
	}
}

// openAIPluginBypassKey 标记必须走原生传输的请求（如 Excel/BPS）：插件自带凭据
// 与目标主机，不能接管这些请求；出口回退、TLS 指纹和计时仍按原生链路执行。
type openAIPluginBypassKey struct{}

func withOpenAIPluginBypass(ctx context.Context) context.Context {
	return context.WithValue(ctx, openAIPluginBypassKey{}, true)
}

func openAIPluginBypassed(ctx context.Context) bool {
	bypassed, _ := ctx.Value(openAIPluginBypassKey{}).(bool)
	return bypassed
}

// codexTicketRequestBound 报告请求是否携带已注入的原生票据快照。
func (s *OpenAIGatewayService) codexTicketRequestBound(req *http.Request, _ *Account) bool {
	if req == nil {
		return false
	}
	receipt, _ := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	return receipt != nil && receipt.ticket.matchesHeaders(req.Header)
}

// codexTicketPinsEgress：票据与签发时的出口绑定（accountCompatible 校验 Egress），
// 运行时代理回退会换出口，因此携带票据快照的请求不参与回退。
func (s *OpenAIGatewayService) codexTicketPinsEgress(req *http.Request, account *Account) bool {
	return s.codexTicketRequestBound(req, account)
}

func (s *OpenAIGatewayService) applyOpenAIRequestTimezone(request *http.Request, account *Account) error {
	if s == nil || s.settingService == nil || request == nil || request.Body == nil || account == nil || !account.IsOpenAI() {
		return nil
	}
	target := s.settingService.GetOpenAIRequestTimezone(request.Context())
	policy, policyEnabled := s.codexRequestStrategyPolicyForScope(request.Context(), codexRequestStrategyConnectionScope(request.Context()))
	if !account.UsesOpenAICodexProtocol() {
		policyEnabled = false
	}
	if target == "" && !policyEnabled {
		return nil
	}
	originalBody := request.Body
	body, err := io.ReadAll(originalBody)
	_ = originalBody.Close()
	if err != nil {
		return err
	}
	if policyEnabled {
		body, _, policyErr := ApplyCodexRequestBodyPolicy(body, policy)
		if policyErr != nil {
			return policyErr
		}
		request.Body = io.NopCloser(bytes.NewReader(body))
		request.ContentLength = int64(len(body))
		request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
		return nil
	}
	rewritten, changed, err := RewriteOpenAIRequestTimezone(body, target)
	if err != nil {
		return err
	}
	if !changed {
		rewritten = body
	}
	request.Body = io.NopCloser(bytes.NewReader(rewritten))
	request.ContentLength = int64(len(rewritten))
	request.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(rewritten)), nil
	}
	return nil
}

// doOpenAIAccountTestUpstream 让 OpenAI OAuth 账号测试与真实转发使用同一插件路径。
// API Key 和未命中插件的账号保持各自原有的 HTTPUpstream 行为。
func (s *AccountTestService) doOpenAIAccountTestUpstream(
	request *http.Request,
	proxyURL string,
	account *Account,
	useTLSFallback bool,
) (response *http.Response, err error) {
	profile, err := resolveMode1TLSProfile(account)
	if err != nil {
		return nil, err
	}
	defer func() {
		if observeRandomProxyHTTPResult(request, account, s.accountRepo, response, err) {
			err = &randomProxyReportedTransportError{err}
		}
	}()
	if s.pluginManager != nil {
		response, handled, err := s.pluginManager.RoundTripOpenAIOAuth(request.Context(), request, proxyURL, account)
		if handled {
			return response, err
		}
	}
	if profile != nil && (s.cfg == nil || s.cfg.Gateway.TLSFingerprint.Enabled) {
		return s.httpUpstream.DoWithTLS(request, proxyURL, account.ID, account.Mode1EffectiveConcurrency(), profile)
	}
	if useTLSFallback && !isMode1ProtectionEnabled(account) && s.tlsFPProfileService != nil && (s.cfg == nil || s.cfg.Gateway.TLSFingerprint.Enabled) {
		return s.httpUpstream.DoWithTLS(
			request,
			proxyURL,
			account.ID,
			account.Concurrency,
			s.tlsFPProfileService.ResolveTLSProfile(account),
		)
	}
	return s.httpUpstream.Do(request, proxyURL, account.ID, account.Concurrency)
}
