package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketIPProtectionStoredLegacyDefaults(t *testing.T) {
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
		delete(protection, "proxy_ip_protection_enabled")
		for _, key := range []string{"proxy_ip_failure_account_threshold", "proxy_ip_failure_window_seconds", "proxy_ip_cooldown_seconds", "proxy_ip_max_rounds"} {
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
		require.Zero(t, repo.writes)
		stored, err := svc.UpdateCodexTicketSettings(context.Background(), loaded)
		require.NoError(t, err)
		require.Equal(t, policy, *stored.Protection)
		restarted := NewSettingService(repo, svc.cfg)
		require.Equal(t, policy, restarted.GetOpenAICodexTicketRuntimeConfig(context.Background(), cfg).TicketProtection())
	}
}

func TestCodexTicketIPProtectionValidationBoundaries(t *testing.T) {
	for _, field := range []struct {
		name string
		max  int
		set  func(*config.CodexTicketProtectionConfig, int)
	}{
		{"accounts", 1000, func(p *config.CodexTicketProtectionConfig, v int) { p.ProxyIPFailureAccountThreshold = v }},
		{"window", 86400, func(p *config.CodexTicketProtectionConfig, v int) { p.ProxyIPFailureWindowSeconds = v }},
		{"cooldown", 86400, func(p *config.CodexTicketProtectionConfig, v int) { p.ProxyIPCooldownSeconds = v }},
		{"rounds", 100, func(p *config.CodexTicketProtectionConfig, v int) { p.ProxyIPMaxRounds = v }},
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
