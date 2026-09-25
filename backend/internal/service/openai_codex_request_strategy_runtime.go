package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

var errCodexRequestStrategyProbeFailed = errors.New("codex request strategy probe failed")

// applyCodexRequestStrategy performs the opt-in HTTP-cookie probe before the
// WS pool dials a new connection. It never replaces a caller supplied
// previous_response_id and never persists the probe response id.
func (s *OpenAIGatewayService) applyCodexRequestStrategy(
	ctx context.Context,
	payload map[string]any,
	account *Account,
	token string,
) (bool, error) {
	if s == nil || s.settingService == nil || account == nil || payload == nil {
		return false, nil
	}
	policy, err := s.settingService.GetCodexRequestStrategyPolicy(ctx)
	if err != nil || !policy.Enabled || policy.Strategy != CodexRequestStrategyCookiePreviousWS {
		return false, nil
	}
	if scope := codexRequestStrategyConnectionScope(ctx); policy.Scope != CodexRequestStrategyScopeAll && policy.Scope != scope {
		return false, nil
	}
	if strings.TrimSpace(openAIWSPayloadString(payload, "previous_response_id")) != "" {
		return false, nil
	}
	if !account.IsOpenAIOAuthLike() || account.IsShadow() || strings.TrimSpace(token) == "" {
		// The strategy is specific to OAuth cookie tickets. Other account types
		// keep their normal flow even when the global policy is enabled.
		return false, nil
	}
	model := strings.TrimSpace(openAIWSPayloadString(payload, "model"))
	if model == "" {
		return false, s.handleCodexRequestStrategyFailure(policy, errors.New("request strategy model is missing"))
	}
	cfg := config.NormalizeOpenAICodexTicketConfig(s.openAICodexTicketConfigForAccount(ctx, account))
	ticket := s.lookupOpenAICodexTicketForConfig(account, model, cfg)
	if ticket == nil || !ticket.usesCookies() || !ticket.cookieUsable(time.Now(), cfg) {
		return false, s.handleCodexRequestStrategyFailure(policy, errors.New("no usable cookie ticket is available"))
	}
	projected, projectionErr := s.prepareCodexCookieTicket(ctx, account, ticket, cfg)
	if projectionErr != nil {
		// 过滤投影验证失败不受续接策略的 fallback 开关影响。
		return false, projectionErr
	}
	if projected == nil {
		return false, s.handleCodexRequestStrategyFailure(policy, errors.New("no usable cookie projection is available"))
	}
	ticket = projected
	responseID := ""
	sessionID := ticket.SessionID
	_, _, probeErr := s.probeOpenAICodexTicket(ctx, openAICodexTicketProbeInput{
		Account:                    account,
		Token:                      token,
		Model:                      model,
		ProxyURL:                   resolveAccountProxyURL(account),
		Timeout:                    time.Duration(policy.ProbeTimeoutSeconds) * time.Second,
		Config:                     &cfg,
		SubscriptionTier:           openAICodexTicketSubscriptionTier(account),
		BusinessCredentialSnapshot: ticket,
		SessionID:                  &sessionID,
		ResponseID:                 &responseID,
		CheckControls:              true,
		FreezeCredentials:          true,
	})
	if probeErr != nil || strings.TrimSpace(responseID) == "" {
		if probeErr == nil {
			probeErr = errCodexRequestStrategyProbeFailed
		}
		return false, s.handleCodexRequestStrategyFailure(policy, probeErr)
	}
	payload["previous_response_id"] = responseID
	return true, nil
}

func (s *OpenAIGatewayService) applyCodexRequestStrategyRaw(ctx context.Context, raw []byte, account *Account, token string) ([]byte, bool, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return raw, false, err
	}
	applied, err := s.applyCodexRequestStrategy(ctx, payload, account, token)
	if err != nil {
		return nil, false, err
	}
	if !applied {
		return raw, false, nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, false, err
	}
	return encoded, applied, nil
}

type codexRequestStrategyScopeKey struct{}

func withCodexRequestStrategyConnectionScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, codexRequestStrategyScopeKey{}, strings.TrimSpace(scope))
}

// codexRequestStrategyScopeForIngressMode 把 WS 入站模式映射为请求策略的生效范围。
// 只有 passthrough 模式属于透传连接；ctx_pool/shared/dedicated/http_bridge 都由网关
// 管理上游连接，归入专用连接范围，避免策略范围与 WS 模式名比较而永远不生效。
func codexRequestStrategyScopeForIngressMode(mode string) string {
	if strings.TrimSpace(mode) == OpenAIWSIngressModePassthrough {
		return CodexRequestStrategyScopePassthrough
	}
	return CodexRequestStrategyScopeDedicated
}

func codexRequestStrategyConnectionScope(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	scope, _ := ctx.Value(codexRequestStrategyScopeKey{}).(string)
	return strings.TrimSpace(scope)
}

func (s *OpenAIGatewayService) handleCodexRequestStrategyFailure(policy CodexRequestStrategyPolicy, err error) error {
	if policy.FailureMode == CodexRequestStrategyReject {
		return err
	}
	return nil
}
