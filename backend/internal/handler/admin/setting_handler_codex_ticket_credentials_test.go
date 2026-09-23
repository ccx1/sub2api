package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketCredentialPolicyHandler(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, nil)
	var initial struct {
		Data config.OpenAICodexTicketConfig `json:"data"`
	}
	require.NoError(t, json.Unmarshal(ticketPolicyRequest(t, h, http.MethodGet, nil).Body.Bytes(), &initial))
	require.Equal(t, "state", initial.Data.CredentialMode)
	require.Equal(t, 20, initial.Data.CookieTTLSeconds)
	require.Equal(t, 5, *initial.Data.CookieRefreshBeforeSeconds)
	for _, mode := range []string{"cookie_state", "cookie", "state"} {
		initial.Data.CredentialMode, initial.Data.CookieTTLSeconds = mode, 15
		refresh := 0
		initial.Data.CookieRefreshBeforeSeconds = &refresh
		body, err := json.Marshal(initial.Data)
		require.NoError(t, err)
		put := ticketPolicyRequest(t, h, http.MethodPut, body)
		require.Equal(t, http.StatusOK, put.Code, put.Body.String())
		require.Equal(t, put.Body.String(), ticketPolicyRequest(t, h, http.MethodGet, nil).Body.String())
		require.Contains(t, repo.values[service.SettingKeyCodexTicketPolicy], `"cookie_refresh_before_seconds":0`)
	}
	before := repo.values[service.SettingKeyCodexTicketPolicy]
	for _, invalid := range []string{
		`"credential_mode":"unknown"`, `"credential_mode":true`, `"cookie_ttl_seconds":-1`, `"cookie_ttl_seconds":3601`,
		`"cookie_ttl_seconds":1.5`, `"cookie_refresh_before_seconds":-1`, `"cookie_refresh_before_seconds":15`, `"cookie_refresh_before_seconds":0.5`,
	} {
		body, err := json.Marshal(initial.Data)
		require.NoError(t, err)
		var value, change map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(body, &value))
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
