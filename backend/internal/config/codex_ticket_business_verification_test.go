//go:build unit

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketBusinessVerificationDefaultsAndExplicitFalse(t *testing.T) {
	require.True(t, CodexTicketBusinessVerificationEnabled(OpenAICodexTicketConfig{}))
	legacy := NormalizeOpenAICodexTicketConfig(OpenAICodexTicketConfig{})
	require.NotNil(t, legacy.VerifyBusiness)
	require.True(t, *legacy.VerifyBusiness)
	require.Equal(t, 1, legacy.BusinessVerificationRounds)
	disabled := false
	normalized := NormalizeOpenAICodexTicketConfig(OpenAICodexTicketConfig{VerifyBusiness: &disabled})
	require.False(t, CodexTicketBusinessVerificationEnabled(normalized))
}

func TestCodexTicketBusinessVerificationRoundsNormalize(t *testing.T) {
	for _, rounds := range []int{0, -1} {
		cfg := NormalizeOpenAICodexTicketConfig(OpenAICodexTicketConfig{BusinessVerificationRounds: rounds})
		require.Equal(t, 1, cfg.BusinessVerificationRounds)
	}
	require.Equal(t, 5, NormalizeOpenAICodexTicketConfig(OpenAICodexTicketConfig{BusinessVerificationRounds: 5}).BusinessVerificationRounds)
	require.Equal(t, 10, NormalizeOpenAICodexTicketConfig(OpenAICodexTicketConfig{BusinessVerificationRounds: 99}).BusinessVerificationRounds)
}

func TestCodexTicketBusinessVerificationEnvironment(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_VERIFY_BUSINESS", "false")
	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg.Gateway.OpenAICodexTicket.VerifyBusiness)
	require.False(t, CodexTicketBusinessVerificationEnabled(cfg.Gateway.OpenAICodexTicket))
}
