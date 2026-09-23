package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketRejectionRetryStoredLegacyDefaults(t *testing.T) {
	for _, includeZero := range []bool{false, true} {
		svc, repo := newTicketPolicySettings()
		cfg, err := svc.GetCodexTicketSettings(context.Background())
		require.NoError(t, err)
		policy := config.DefaultCodexTicketProtection()
		cfg.Protection = &policy
		encoded, err := json.Marshal(cfg)
		require.NoError(t, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(encoded, &body))
		protection := body["protection"].(map[string]any)
		for _, key := range []string{"rejection_retry_interval_seconds", "rejection_retry_max_attempts", "rejection_retry_cooldown_seconds"} {
			delete(protection, key)
			if includeZero {
				protection[key] = 0
			}
		}
		encoded, err = json.Marshal(body)
		require.NoError(t, err)
		repo.values[SettingKeyCodexTicketPolicy] = string(encoded)
		loaded, err := svc.GetCodexTicketSettings(context.Background())
		require.NoError(t, err)
		require.Equal(t, policy, *loaded.Protection)
		require.Zero(t, repo.writes, "reading legacy settings must not persist defaults")
		stored, err := svc.UpdateCodexTicketSettings(context.Background(), loaded)
		require.NoError(t, err)
		require.Equal(t, policy, *stored.Protection)
		require.Equal(t, policy, svc.GetOpenAICodexTicketRuntimeConfig(context.Background(), cfg).TicketProtection())
	}
}

func TestCodexTicketRejectionRetryValidationBoundaries(t *testing.T) {
	for _, field := range []struct {
		name string
		max  int
		set  func(*config.CodexTicketProtectionConfig, int)
	}{
		{"interval", 86400, func(p *config.CodexTicketProtectionConfig, value int) { p.RejectionRetryIntervalSeconds = value }},
		{"attempts", 1000, func(p *config.CodexTicketProtectionConfig, value int) { p.RejectionRetryMaxAttempts = value }},
		{"cooldown", 86400, func(p *config.CodexTicketProtectionConfig, value int) { p.RejectionRetryCooldownSeconds = value }},
	} {
		t.Run(field.name, func(t *testing.T) {
			for _, value := range []int{-1, 0, 1, field.max, field.max + 1} {
				svc, repo := newTicketPolicySettings()
				cfg, err := svc.GetCodexTicketSettings(context.Background())
				require.NoError(t, err)
				policy := config.DefaultCodexTicketProtection()
				field.set(&policy, value)
				cfg.Protection = &policy
				_, err = svc.UpdateCodexTicketSettings(context.Background(), cfg)
				valid := value >= 0 && value <= field.max
				require.Equal(t, valid, err == nil, "value=%d: %v", value, err)
				require.Equal(t, valid, repo.writes == 1)
			}
		})
	}
}
