package service

import (
	"bytes"
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
	nativeTicketResponse := true
	defer func() {
		if nativeTicketResponse {
			s.observeOpenAICodexTicketResponse(request, response)
		}
	}()
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
			nativeTicketResponse = false
			return response, err
		}
	}
	if profile != nil && (s.cfg == nil || s.cfg.Gateway.TLSFingerprint.Enabled) {
		return s.httpUpstream.DoWithTLS(request, proxyURL, account.ID, account.Mode1EffectiveConcurrency(), profile)
	}
	return s.httpUpstream.Do(request, proxyURL, account.ID, account.Mode1EffectiveConcurrency())
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
