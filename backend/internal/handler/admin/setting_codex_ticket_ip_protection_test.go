package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketIPProtectionHandlerRoundTrip(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, nil)
	var initial struct {
		Data config.OpenAICodexTicketConfig `json:"data"`
	}
	require.NoError(t, json.Unmarshal(ticketPolicyRequest(t, h, http.MethodGet, nil).Body.Bytes(), &initial))
	policy := config.DefaultCodexTicketProtection()
	policy.ProxyIPProtectionEnabled = true
	policy.ProxyIPFailureAccountThreshold, policy.ProxyIPFailureWindowSeconds = 7, 120
	policy.ProxyIPCooldownSeconds, policy.ProxyIPMaxRounds = 900, 4
	initial.Data.Protection = &policy
	for _, enabled := range []bool{true, false, true} {
		initial.Data.Protection.ProxyIPProtectionEnabled = enabled
		body, err := json.Marshal(initial.Data)
		require.NoError(t, err)
		put := ticketPolicyRequest(t, h, http.MethodPut, body)
		require.Equal(t, http.StatusOK, put.Code, put.Body.String())
		require.Equal(t, put.Body.String(), ticketPolicyRequest(t, h, http.MethodGet, nil).Body.String())
		var stored config.OpenAICodexTicketConfig
		require.NoError(t, json.Unmarshal([]byte(repo.values[service.SettingKeyCodexTicketPolicy]), &stored))
		require.Equal(t, *initial.Data.Protection, *stored.Protection)
		require.False(t, stored.Protection.Enabled)
	}
}

func TestCodexTicketIPProtectionHandlerRejectsInvalidPayload(t *testing.T) {
	for field, values := range map[string][]any{
		"proxy_ip_protection_enabled":        {"true", 1, []any{}},
		"proxy_ip_failure_account_threshold": {-1, 1001, 1.5, "3", true},
		"proxy_ip_failure_window_seconds":    {-1, 86401, 1.5, "600", true},
		"proxy_ip_cooldown_seconds":          {-1, 86401, 1.5, "300", true},
		"proxy_ip_max_rounds":                {-1, 101, 1.5, "3", true},
	} {
		for _, invalid := range values {
			h, repo := newStepUpSwitchTestHandler(t, nil)
			cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{})
			policy := config.DefaultCodexTicketProtection()
			cfg.Protection = &policy
			raw, err := json.Marshal(cfg)
			require.NoError(t, err)
			var body map[string]any
			require.NoError(t, json.Unmarshal(raw, &body))
			body["protection"].(map[string]any)[field] = invalid
			raw, err = json.Marshal(body)
			require.NoError(t, err)
			put := ticketPolicyRequest(t, h, http.MethodPut, raw)
			require.Equal(t, http.StatusBadRequest, put.Code, "%s=%v: %s", field, invalid, put.Body.String())
			require.Empty(t, repo.values[service.SettingKeyCodexTicketPolicy])
		}
	}
}
