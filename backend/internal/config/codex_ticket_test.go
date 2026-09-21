//go:build unit

package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadCodexTicketPolicyDefaultsAndEnvironment(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_HARVEST_CONCURRENCY", "3")
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_RETRY_BACKOFF_SECONDS", "12,24,48")
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_RETRY_MAX_ATTEMPTS", "0")
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_AUTH_COOLDOWN_SECONDS", "90")
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_RATE_LIMIT_COOLDOWN_SECONDS", "120")
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_RESPECT_RETRY_AFTER", "false")
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_REFRESH_BEFORE_SECONDS", "0")
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_LENGTH_MODE", "auto")
	cfg, err := Load()
	require.NoError(t, err)
	policy := NormalizeOpenAICodexTicketConfig(cfg.Gateway.OpenAICodexTicket)
	require.Equal(t, CodexTicketLengthAuto, policy.LengthMode)
	require.Equal(t, 3, policy.HarvestConcurrency)
	require.Equal(t, []int{12, 24, 48}, policy.RetryBackoffSeconds)
	require.Zero(t, policy.RetryMaxAttempts)
	require.Zero(t, policy.RefreshBeforeSeconds)
	require.Equal(t, 90, policy.AuthCooldownSeconds)
	require.Equal(t, 120, policy.RateLimitCooldownSeconds)
	require.False(t, policy.RespectRetryAfter)
	require.Equal(t, 332, policy.TierRules[0].TargetLength)
	require.Equal(t, 292, policy.TierRules[1].TargetLength)
	require.Equal(t, []int{312}, policy.RejectedLengths)
}

func TestNormalizeCodexTicketLengthModePreservesLegacyRules(t *testing.T) {
	legacy := NormalizeOpenAICodexTicketConfig(OpenAICodexTicketConfig{})
	require.Equal(t, CodexTicketLengthStrict, legacy.LengthMode)
	require.Equal(t, []int{312}, legacy.RejectedLengths)
	dynamic := NormalizeOpenAICodexTicketConfig(OpenAICodexTicketConfig{LengthMode: CodexTicketLengthAuto})
	require.Equal(t, CodexTicketLengthAuto, dynamic.LengthMode)
}
