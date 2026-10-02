package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketPolicyLoadPreservesLegacyDefaults(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	ticket := cfg.Gateway.OpenAICodexTicket
	require.Nil(t, ticket.Protection)
	require.Equal(t, DefaultCodexTicketProtection(), ticket.TicketProtection())

	normalized := NormalizeOpenAICodexTicketConfig(ticket)
	require.Equal(t, CodexTicketRejectSafetyBufferingOff, normalized.RejectSafetyBuffering)
	require.Equal(t, CodexTicketWorkspaceOriginRoutingOff, normalized.WorkspaceOriginRouting)
	require.Equal(t, 3, normalized.WorkspaceOriginFailureThreshold)
	require.Equal(t, 600, normalized.WorkspaceOriginSilenceSeconds)
}

func TestCodexTicketPolicyLoadPreservesPartialYAML(t *testing.T) {
	resetViperWithJWTSecret(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`gateway:
  openai_codex_ticket:
    protection:
      enabled: true
      reject_and_silence_lengths: []
      max_pool_rounds: 0
      max_account_attempts: 8
`), 0o600))
	t.Setenv("CONFIG_FILE", path)
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, &CodexTicketProtectionConfig{
		Enabled: true, RejectAndSilenceLengths: []int{}, MaxAccountAttempts: 8,
	}, cfg.Gateway.OpenAICodexTicket.Protection)
}

func TestCodexTicketProtectionLoadFromEnvironment(t *testing.T) {
	resetViperWithJWTSecret(t)
	for key, value := range map[string]string{
		"ENABLED": "true", "REJECT_AND_SILENCE_LENGTHS": "312,356",
		"PROXY_SILENCE_SECONDS": "120", "MAX_POOL_ROUNDS": "4",
		"MAX_ACCOUNT_ATTEMPTS": "9", "ACCOUNT_COOLDOWN_SECONDS": "900",
		"REJECTION_RETRY_INTERVAL_SECONDS": "45", "REJECTION_RETRY_MAX_ATTEMPTS": "8",
		"REJECTION_RETRY_COOLDOWN_SECONDS": "240", "PROXY_IP_PROTECTION_ENABLED": "true",
		"PROXY_IP_FAILURE_ACCOUNT_THRESHOLD": "5", "PROXY_IP_FAILURE_WINDOW_SECONDS": "180",
		"PROXY_IP_COOLDOWN_SECONDS": "420", "PROXY_IP_MAX_ROUNDS": "6",
	} {
		t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_PROTECTION_"+key, value)
	}
	cfg, err := Load()
	require.NoError(t, err)
	want := CodexTicketProtectionConfig{
		Enabled: true, RejectAndSilenceLengths: []int{312, 356},
		ProxySilenceSeconds: 120, MaxPoolRounds: 4, MaxAccountAttempts: 9, AccountCooldownSeconds: 900,
		RejectionRetryIntervalSeconds: 45, RejectionRetryMaxAttempts: 8, RejectionRetryCooldownSeconds: 240,
		ProxyIPProtectionEnabled: true, ProxyIPFailureAccountThreshold: 5, ProxyIPFailureWindowSeconds: 180,
		ProxyIPCooldownSeconds: 420, ProxyIPMaxRounds: 6,
	}
	require.Equal(t, &want, cfg.Gateway.OpenAICodexTicket.Protection)
	require.Equal(t, want, cfg.Gateway.OpenAICodexTicket.TicketProtection())
}

func TestCodexTicketProtectionEnvironmentOverridesYAMLWithFalseAndZero(t *testing.T) {
	resetViperWithJWTSecret(t)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`gateway:
  openai_codex_ticket:
    protection:
      enabled: true
      proxy_ip_protection_enabled: true
      max_pool_rounds: 4
      max_account_attempts: 8
`), 0o600))
	t.Setenv("CONFIG_FILE", path)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_PROTECTION_ENABLED", "false")
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_PROTECTION_PROXY_IP_PROTECTION_ENABLED", "false")
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_PROTECTION_MAX_POOL_ROUNDS", "0")
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, &CodexTicketProtectionConfig{MaxAccountAttempts: 8}, cfg.Gateway.OpenAICodexTicket.Protection)
}

func TestCodexTicketRoutingPolicyLoadFromEnvironment(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_REJECT_SAFETY_BUFFERING", CodexTicketRejectSafetyBufferingAny)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_WORKSPACE_ORIGIN_ROUTING", CodexTicketWorkspaceOriginRoutingProbe)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_WORKSPACE_ORIGIN_FAILURE_THRESHOLD", "5")
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_WORKSPACE_ORIGIN_SILENCE_SECONDS", "120")
	cfg, err := Load()
	require.NoError(t, err)
	ticket := NormalizeOpenAICodexTicketConfig(cfg.Gateway.OpenAICodexTicket)
	require.Equal(t, CodexTicketRejectSafetyBufferingAny, ticket.RejectSafetyBuffering)
	require.Equal(t, CodexTicketWorkspaceOriginRoutingProbe, ticket.WorkspaceOriginRouting)
	require.Equal(t, 5, ticket.WorkspaceOriginFailureThreshold)
	require.Equal(t, 120, ticket.WorkspaceOriginSilenceSeconds)
}
