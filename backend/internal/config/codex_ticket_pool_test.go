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

func TestCodexTicketAccountPoolCapacitySplitsAcrossModels(t *testing.T) {
	cfg := NormalizeOpenAICodexTicketConfig(OpenAICodexTicketConfig{
		Models: []string{"first", "second", "third"}, AccountPoolCapacity: 1000,
	})
	require.Equal(t, 334, CodexTicketModelCapacity(cfg, "first"))
	require.Equal(t, 333, CodexTicketModelCapacity(cfg, "second"))
	require.Equal(t, 333, CodexTicketModelCapacity(cfg, "third"))
	require.Zero(t, CodexTicketModelCapacity(cfg, "removed"))
	require.NoError(t, ValidateCodexTicketUsage(&cfg))
	cfg.AccountPoolCapacity = 2
	require.ErrorContains(t, ValidateCodexTicketUsage(&cfg), "account_pool_capacity")
	cfg.AccountPoolCapacity = 0
	require.NoError(t, ValidateCodexTicketUsage(&cfg))
	require.Equal(t, DefaultCodexTicketPoolCapacity, CodexTicketModelCapacity(cfg, "third"))
}
