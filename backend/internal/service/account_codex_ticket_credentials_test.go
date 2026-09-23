package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAccountCodexTicketCredentialsResolveInheritanceAndOverrides(t *testing.T) {
	base := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{FailClosed: true})
	for _, test := range []struct {
		name, global, mode string
		policy             any
		ttl, refresh       int
		closed             bool
	}{
		{"legacy", "state", "state", nil, 3600, 600, true},
		{"global_cookie", "cookie", "cookie", nil, 3600, 5, true},
		{"inherit", "cookie_state", "cookie_state", map[string]any{"mode": "inherit", "ttl_seconds": 99}, 3600, 5, true},
		{"account_state", "cookie", "state", map[string]any{"mode": "state"}, 3600, 600, true},
		{"account_cookie", "state", "cookie", map[string]any{"mode": "cookie", "ttl_seconds": 12, "refresh_before_seconds": 0}, 3600, 0, true},
		{"shorter_ttl", "state", "cookie_state", map[string]any{"mode": "cookie_state", "ttl_seconds": 2}, 3600, 1, true},
		{"inherited_ttl", "cookie", "cookie", map[string]any{"mode": "cookie", "refresh_before_seconds": 30}, 3600, 19, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := base
			cfg.CredentialMode = test.global
			account := &Account{Extra: map[string]any{CodexTicketCredentialPolicyExtraKey: test.policy}}
			resolved := resolveCodexTicketCredentialConfig(account, cfg)
			require.Equal(t, test.mode, resolved.CredentialMode)
			require.Equal(t, test.ttl, resolved.TTLSeconds)
			require.Equal(t, test.refresh, resolved.RefreshBeforeSeconds)
			require.Equal(t, test.closed, resolved.FailClosed)
			require.Equal(t, resolved, resolveCodexTicketCredentialConfig(account, resolved))
			require.Equal(t, 5, *base.CookieRefreshBeforeSeconds)
		})
	}
}

func TestAccountCodexTicketCredentialPolicyValidation(t *testing.T) {
	for name, policy := range map[string]any{
		"missing": nil, "inherit": map[string]any{"mode": "inherit"},
		"state": map[string]any{"mode": "state"}, "cookie": map[string]any{"mode": "cookie", "ttl_seconds": 1, "refresh_before_seconds": 0},
		"both": map[string]any{"mode": "cookie_state", "ttl_seconds": 3600, "refresh_before_seconds": 3599},
	} {
		t.Run(name, func(t *testing.T) {
			require.NoError(t, ValidateCodexTicketProxyExtra(map[string]any{CodexTicketCredentialPolicyExtraKey: policy}))
		})
	}
	for name, policy := range map[string]any{
		"string": "cookie", "array": []any{}, "empty": map[string]any{}, "null_mode": map[string]any{"mode": nil},
		"unknown_mode": map[string]any{"mode": "unexpected"}, "unknown_field": map[string]any{"mode": "cookie", "secret": true},
		"zero_ttl": map[string]any{"mode": "cookie", "ttl_seconds": 0}, "negative_ttl": map[string]any{"mode": "cookie", "ttl_seconds": -1},
		"large_ttl": map[string]any{"mode": "cookie", "ttl_seconds": 3601}, "string_ttl": map[string]any{"mode": "cookie", "ttl_seconds": "20"},
		"fraction_ttl": map[string]any{"mode": "cookie", "ttl_seconds": 1.5}, "negative_refresh": map[string]any{"mode": "cookie", "refresh_before_seconds": -1},
		"equal_window": map[string]any{"mode": "cookie", "ttl_seconds": 20, "refresh_before_seconds": 20},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, ValidateCodexTicketProxyExtra(map[string]any{CodexTicketCredentialPolicyExtraKey: policy}))
		})
	}
}

func TestAccountCodexTicketCredentialPolicyPartialUpdateAndClear(t *testing.T) {
	policy := map[string]any{"mode": "cookie", "ttl_seconds": 15}
	current := map[string]any{CodexTicketCredentialPolicyExtraKey: policy}
	merged, err := normalizeCodexTicketProxyUpdate(map[string]any{"custom": true}, current)
	require.NoError(t, err)
	require.Equal(t, policy, merged[CodexTicketCredentialPolicyExtraKey])
	require.NotContains(t, codexTicketProxyExplicitExtra(merged, map[string]any{"custom": true}), CodexTicketCredentialPolicyExtraKey)
	for _, clear := range []any{nil, map[string]any{"mode": "inherit", "ttl_seconds": 15, "refresh_before_seconds": 1}} {
		requested := map[string]any{CodexTicketCredentialPolicyExtraKey: clear}
		merged, err := normalizeCodexTicketProxyUpdate(requested, current)
		require.NoError(t, err)
		if clear == nil {
			require.Nil(t, merged[CodexTicketCredentialPolicyExtraKey])
		} else {
			require.Equal(t, map[string]any{"mode": "inherit"}, merged[CodexTicketCredentialPolicyExtraKey])
		}
		require.True(t, CodexTicketCredentialPolicyWrite(WithCodexTicketProxyWrite(context.Background(), requested)))
	}
	require.False(t, CodexTicketCredentialPolicyWrite(WithCodexTicketProxyWrite(context.Background(), map[string]any{"custom": true})))
	require.Equal(t, policy, current[CodexTicketCredentialPolicyExtraKey])
}

func TestCodexTicketCredentialSettingsCloneIsolation(t *testing.T) {
	cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{})
	cloned := cloneCodexTicketSettings(cfg)
	*cloned.CookieRefreshBeforeSeconds = 0
	require.Equal(t, 5, *cfg.CookieRefreshBeforeSeconds)
}
