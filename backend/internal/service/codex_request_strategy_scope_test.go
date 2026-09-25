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
