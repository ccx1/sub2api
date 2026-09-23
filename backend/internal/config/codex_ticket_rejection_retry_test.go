package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketRejectionRetryLegacyDefaults(t *testing.T) {
	for _, raw := range []string{`{}`, `{"protection":{}}`, `{"protection":{"rejection_retry_interval_seconds":0,"rejection_retry_max_attempts":0,"rejection_retry_cooldown_seconds":0}}`} {
		var cfg OpenAICodexTicketConfig
		require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
		policy := cfg.TicketProtection()
		require.Equal(t, 30, policy.RejectionRetryIntervalSeconds)
		require.Equal(t, 6, policy.RejectionRetryMaxAttempts)
		require.Equal(t, 300, policy.RejectionRetryCooldownSeconds)
	}
}

func TestCodexTicketRejectionRetryPreservesExplicitValues(t *testing.T) {
	policy := DefaultCodexTicketProtection()
	policy.RejectionRetryIntervalSeconds, policy.RejectionRetryMaxAttempts, policy.RejectionRetryCooldownSeconds = 45, 8, 600
	cfg := OpenAICodexTicketConfig{Protection: &policy}
	require.Equal(t, policy, cfg.TicketProtection())
	policy.RejectionRetryIntervalSeconds = -1
	require.Equal(t, -1, cfg.TicketProtection().RejectionRetryIntervalSeconds, "negative settings must reach validation")
}
