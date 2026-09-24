package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketIPProtectionLegacyDefaults(t *testing.T) {
	for _, raw := range []string{`{}`, `{"protection":{}}`, `{"protection":{"proxy_ip_failure_account_threshold":0,"proxy_ip_failure_window_seconds":0,"proxy_ip_cooldown_seconds":0,"proxy_ip_max_rounds":0}}`} {
		var cfg OpenAICodexTicketConfig
		require.NoError(t, json.Unmarshal([]byte(raw), &cfg))
		policy := cfg.TicketProtection()
		require.False(t, policy.ProxyIPProtectionEnabled)
		require.Equal(t, 3, policy.ProxyIPFailureAccountThreshold)
		require.Equal(t, 600, policy.ProxyIPFailureWindowSeconds)
		require.Equal(t, 300, policy.ProxyIPCooldownSeconds)
		require.Equal(t, 3, policy.ProxyIPMaxRounds)
	}
}

func TestCodexTicketIPProtectionExplicitPolicyAndSnapshot(t *testing.T) {
	policy := DefaultCodexTicketProtection()
	policy.ProxyIPProtectionEnabled = true
	policy.ProxyIPFailureAccountThreshold, policy.ProxyIPFailureWindowSeconds = 5, 120
	policy.ProxyIPCooldownSeconds, policy.ProxyIPMaxRounds = 900, 4
	cfg := OpenAICodexTicketConfig{Protection: &policy}
	snapshot := cfg.TicketProtection()
	require.Equal(t, policy, snapshot)
	require.False(t, snapshot.Enabled, "IP protection does not enable shared harvest protection")
	snapshot.ProxyIPFailureWindowSeconds = 50
	require.Equal(t, 120, policy.ProxyIPFailureWindowSeconds)
	policy.ProxyIPFailureWindowSeconds = -1
	require.Equal(t, -1, cfg.TicketProtection().ProxyIPFailureWindowSeconds, "invalid values must reach validation")
}
