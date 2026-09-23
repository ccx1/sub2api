package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketPoolDefaultsAndEnvironment(t *testing.T) {
	require.Equal(t, 5, NormalizeOpenAICodexTicketConfig(OpenAICodexTicketConfig{}).PoolCapacity)
	require.Equal(t, 20, NormalizeOpenAICodexTicketConfig(OpenAICodexTicketConfig{PoolCapacity: 99}).PoolCapacity)
	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_POOL_CAPACITY", "8")
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, 8, cfg.Gateway.OpenAICodexTicket.PoolCapacity)
}
