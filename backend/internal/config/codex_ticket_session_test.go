package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketSessionAndProxyDefaults(t *testing.T) {
	cfg := NormalizeOpenAICodexTicketConfig(OpenAICodexTicketConfig{})
	require.Equal(t, CodexTicketSessionRandom, cfg.SessionMode)
	require.Equal(t, 3, cfg.ProxyFailureThreshold)
	for _, mode := range []string{CodexTicketSessionRandom, CodexTicketSessionAccount, CodexTicketSessionAccountModel} {
		cfg := NormalizeOpenAICodexTicketConfig(OpenAICodexTicketConfig{SessionMode: mode, ProxyFailureThreshold: 7})
		require.Equal(t, mode, cfg.SessionMode)
		require.Equal(t, 7, cfg.ProxyFailureThreshold)
	}
}

func TestCodexTicketSessionAndProxyEnvironment(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_SESSION_MODE", "account_model")
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_PROXY_FAILURE_THRESHOLD", "8")
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "account_model", cfg.Gateway.OpenAICodexTicket.SessionMode)
	require.Equal(t, 8, cfg.Gateway.OpenAICodexTicket.ProxyFailureThreshold)
}
