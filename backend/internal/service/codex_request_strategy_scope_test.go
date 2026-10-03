package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexRequestStrategyScopeForIngressMode(t *testing.T) {
	cases := map[string]string{
		OpenAIWSIngressModeCtxPool:     CodexRequestStrategyScopeDedicated,
		OpenAIWSIngressModeShared:      CodexRequestStrategyScopeDedicated,
		OpenAIWSIngressModeDedicated:   CodexRequestStrategyScopeDedicated,
		OpenAIWSIngressModeHTTPBridge:  CodexRequestStrategyScopeDedicated,
		OpenAIWSIngressModePassthrough: CodexRequestStrategyScopePassthrough,
	}
	for mode, want := range cases {
		require.Equal(t, want, codexRequestStrategyScopeForIngressMode(mode), mode)
	}
}

func TestCodexRequestStrategySkipsHistoricalTicketVerification(t *testing.T) {
	cfg := historicalTestConfig(1)
	s := ticketTestService(t, cfg, nil)
	s.settingService = &SettingService{settingRepo: &modelQualitySettingsRepo{}, cfg: s.cfg}
	policy := DefaultCodexRequestStrategyPolicy()
	policy.Enabled, policy.Strategy = true, CodexRequestStrategyCookiePreviousWS
	policy.Scope, policy.FailureMode = CodexRequestStrategyScopeAll, CodexRequestStrategyReject
	_, err := s.settingService.UpdateCodexRequestStrategyPolicy(context.Background(), policy)
	require.NoError(t, err)
	account := ticketTestAccount(41)
	payload := map[string]any{"model": usageTestModel}
	applied, err := s.applyCodexRequestStrategy(context.Background(), payload, account, "token")
	require.NoError(t, err)
	require.False(t, applied)
	require.NotContains(t, payload, "previous_response_id")
}

func TestCodexRequestStrategyDedicatedScopeCoversCtxPoolIngress(t *testing.T) {
	s := &OpenAIGatewayService{settingService: &SettingService{settingRepo: &modelQualitySettingsRepo{}}}
	policy := DefaultCodexRequestStrategyPolicy()
	policy.Enabled, policy.Scope = true, CodexRequestStrategyScopeDedicated
	_, err := s.settingService.UpdateCodexRequestStrategyPolicy(context.Background(), policy)
	require.NoError(t, err)
	ctx := withCodexRequestStrategyConnectionScope(context.Background(), codexRequestStrategyScopeForIngressMode(OpenAIWSIngressModeCtxPool))
	_, enabled := s.codexRequestStrategyPolicyForScope(ctx, codexRequestStrategyConnectionScope(ctx))
	require.True(t, enabled, "专用连接范围必须覆盖 ctx_pool WS 入站")
	ctx = withCodexRequestStrategyConnectionScope(context.Background(), codexRequestStrategyScopeForIngressMode(OpenAIWSIngressModePassthrough))
	_, enabled = s.codexRequestStrategyPolicyForScope(ctx, codexRequestStrategyConnectionScope(ctx))
	require.False(t, enabled, "专用连接范围不应覆盖透传 WS 入站")
}
