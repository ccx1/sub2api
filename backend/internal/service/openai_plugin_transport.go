package service

import "net/http"

func (s *OpenAIGatewayService) SetPluginManager(manager *PluginManager) {
	s.pluginManager = manager
}

// doOpenAIUpstream 只在 OpenAI OAuth 能力绑定已启用时把真实请求交给插件。
// 插件返回标准 http.Response，响应解析、错误映射、SSE 和计费仍由现有核心链处理。
func (s *OpenAIGatewayService) doOpenAIUpstream(request *http.Request, proxyURL string, account *Account) (response *http.Response, err error) {
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
