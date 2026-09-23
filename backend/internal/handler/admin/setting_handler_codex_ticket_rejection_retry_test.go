package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketRejectionRetryHandlerRoundTrip(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, nil)
	var initial struct {
		Data config.OpenAICodexTicketConfig `json:"data"`
	}
	require.NoError(t, json.Unmarshal(ticketPolicyRequest(t, h, http.MethodGet, nil).Body.Bytes(), &initial))
	policy := config.DefaultCodexTicketProtection()
	policy.RejectionRetryIntervalSeconds, policy.RejectionRetryMaxAttempts, policy.RejectionRetryCooldownSeconds = 45, 8, 600
	initial.Data.Protection = &policy
	body, err := json.Marshal(initial.Data)
	require.NoError(t, err)
	put := ticketPolicyRequest(t, h, http.MethodPut, body)
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	require.Equal(t, put.Body.String(), ticketPolicyRequest(t, h, http.MethodGet, nil).Body.String())
	var stored config.OpenAICodexTicketConfig
	require.NoError(t, json.Unmarshal([]byte(repo.values[service.SettingKeyCodexTicketPolicy]), &stored))
	require.Equal(t, policy, *stored.Protection)
}

func TestCodexTicketRejectionRetryHandlerRejectsInvalidPayload(t *testing.T) {
	for _, field := range []string{"rejection_retry_interval_seconds", "rejection_retry_max_attempts", "rejection_retry_cooldown_seconds"} {
		for _, invalid := range []any{-1, 86401, 1.5, "30", true} {
			h, repo := newStepUpSwitchTestHandler(t, nil)
			var body map[string]any
			cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{})
			policy := config.DefaultCodexTicketProtection()
			cfg.Protection = &policy
			raw, err := json.Marshal(cfg)
			require.NoError(t, err)
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
