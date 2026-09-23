package admin

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketBusinessVerificationHandlerRoundTrip(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, nil)
	get := ticketPolicyRequest(t, h, http.MethodGet, nil)
	var initial struct {
		Data config.OpenAICodexTicketConfig `json:"data"`
	}
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &initial))
	require.NotNil(t, initial.Data.VerifyBusiness)
	require.True(t, *initial.Data.VerifyBusiness)
	for _, enabled := range []bool{false, true} {
		initial.Data.VerifyBusiness = &enabled
		body, err := json.Marshal(initial.Data)
		require.NoError(t, err)
		put := ticketPolicyRequest(t, h, http.MethodPut, body)
		require.Equal(t, http.StatusOK, put.Code, put.Body.String())
		require.Equal(t, put.Body.String(), ticketPolicyRequest(t, h, http.MethodGet, nil).Body.String())
		var stored config.OpenAICodexTicketConfig
		require.NoError(t, json.Unmarshal([]byte(repo.values[service.SettingKeyCodexTicketPolicy]), &stored))
		require.Equal(t, enabled, *stored.VerifyBusiness)
	}
}

func TestCodexTicketBusinessVerificationHandlerRejectsInvalidType(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, nil)
	for _, body := range []string{`{"verify_business":"false"}`, `{"verify_business":0}`, `{"verify_business":[]}`, `{"verify_business":{}}`} {
		put := ticketPolicyRequest(t, h, http.MethodPut, []byte(body))
		require.Equal(t, http.StatusBadRequest, put.Code, body)
		require.Empty(t, repo.values[service.SettingKeyCodexTicketPolicy])
	}
}
