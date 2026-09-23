package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketRefreshStrategyHandlerRoundTrip(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, nil)
	var initial struct {
		Data config.OpenAICodexTicketConfig `json:"data"`
	}
	get := ticketPolicyRequest(t, h, http.MethodGet, nil)
	require.Equal(t, http.StatusOK, get.Code)
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &initial))
	require.Equal(t, config.CodexTicketRefreshRevalidate, initial.Data.RefreshStrategy)
	for _, strategy := range []string{config.CodexTicketRefreshReplace, config.CodexTicketRefreshRevalidate} {
		initial.Data.RefreshStrategy = strategy
		body, err := json.Marshal(initial.Data)
		require.NoError(t, err)
		put := ticketPolicyRequest(t, h, http.MethodPut, body)
		require.Equal(t, http.StatusOK, put.Code, put.Body.String())
		require.Equal(t, put.Body.String(), ticketPolicyRequest(t, h, http.MethodGet, nil).Body.String())
		var persisted config.OpenAICodexTicketConfig
		require.NoError(t, json.Unmarshal([]byte(repo.values[service.SettingKeyCodexTicketPolicy]), &persisted))
		require.Equal(t, strategy, persisted.RefreshStrategy)
	}
}

func TestCodexTicketRefreshStrategyHandlerRejectsInvalidPayload(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, nil)
	cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{})
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(raw, &payload))
	for _, value := range []any{"wait_for_ticket", 1, true, []string{"replace"}} {
		payload["refresh_strategy"] = value
		body, err := json.Marshal(payload)
		require.NoError(t, err)
		put := ticketPolicyRequest(t, h, http.MethodPut, body)
		require.Equal(t, http.StatusBadRequest, put.Code, put.Body.String())
		require.Empty(t, repo.values[service.SettingKeyCodexTicketPolicy])
	}
}
