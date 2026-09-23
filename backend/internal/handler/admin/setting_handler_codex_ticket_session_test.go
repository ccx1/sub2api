package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketSessionAndProxyPolicyHandler(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, nil)
	var initial struct {
		Data config.OpenAICodexTicketConfig `json:"data"`
	}
	get := ticketPolicyRequest(t, h, http.MethodGet, nil)
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &initial))
	require.Equal(t, "random", initial.Data.SessionMode)
	require.Equal(t, 3, initial.Data.ProxyFailureThreshold)
	for _, mode := range []string{"account", "account_model", "random"} {
		initial.Data.SessionMode, initial.Data.ProxyFailureThreshold = mode, 5
		body, err := json.Marshal(initial.Data)
		require.NoError(t, err)
		put := ticketPolicyRequest(t, h, http.MethodPut, body)
		require.Equal(t, http.StatusOK, put.Code, put.Body.String())
		require.Equal(t, put.Body.String(), ticketPolicyRequest(t, h, http.MethodGet, nil).Body.String())
		require.Contains(t, repo.values[service.SettingKeyCodexTicketPolicy], `"proxy_failure_threshold":5`)
	}
	before := repo.values[service.SettingKeyCodexTicketPolicy]
	for _, invalid := range []string{`"session_mode":"unknown"`, `"session_mode":true`, `"proxy_failure_threshold":-1`, `"proxy_failure_threshold":1001`, `"proxy_failure_threshold":1.5`} {
		body, err := json.Marshal(initial.Data)
		require.NoError(t, err)
		var value map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &value))
		var change map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte("{"+invalid+"}"), &change))
		for key, raw := range change {
			value[key] = raw
		}
		body, err = json.Marshal(value)
		require.NoError(t, err)
		require.Equal(t, http.StatusBadRequest, ticketPolicyRequest(t, h, http.MethodPut, body).Code)
		require.Equal(t, before, repo.values[service.SettingKeyCodexTicketPolicy])
	}
}
