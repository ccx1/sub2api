package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketRefreshStrategyDefaultsAndExplicitModes(t *testing.T) {
	for _, mode := range []string{"", CodexTicketRefreshRevalidate, CodexTicketRefreshReplace} {
		cfg := NormalizeOpenAICodexTicketConfig(OpenAICodexTicketConfig{RefreshStrategy: mode})
		if mode == "" {
			require.Equal(t, CodexTicketRefreshRevalidate, cfg.RefreshStrategy)
		} else {
			require.Equal(t, mode, cfg.RefreshStrategy)
		}
	}
}

func TestCodexTicketRefreshStrategyEnvironmentAndValidation(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_REFRESH_STRATEGY", CodexTicketRefreshReplace)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, CodexTicketRefreshReplace, cfg.Gateway.OpenAICodexTicket.RefreshStrategy)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_REFRESH_STRATEGY", "wait_for_ticket")
	_, err = Load()
	require.ErrorContains(t, err, "自动更新策略")
}
