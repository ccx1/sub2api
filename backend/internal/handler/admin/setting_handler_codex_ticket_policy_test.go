package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func ticketPolicyRequest(t *testing.T, h *SettingHandler, method string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, "/api/v1/admin/settings/codex-tickets", bytes.NewReader(body))
	if method == http.MethodGet {
		h.GetCodexTicketSettings(c)
	} else {
		h.UpdateCodexTicketSettings(c)
	}
	return w
}

func TestCodexTicketPolicyHandlerRoundTrip(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{service.SettingKeyOpenAICodexTicketHarvestProxyURL: "http://user:private-secret@proxy.test:8080"})
	get := ticketPolicyRequest(t, h, http.MethodGet, nil)
	require.Equal(t, http.StatusOK, get.Code, get.Body.String())
	var initial struct {
		Data config.OpenAICodexTicketConfig `json:"data"`
	}
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &initial))
	initial.Data.Enabled = true
	initial.Data.TierRules[0].TargetLength = 352
	initial.Data.RetryBackoffSeconds = []int{15, 30, 60}
	body, err := json.Marshal(initial.Data)
	require.NoError(t, err)
	put := ticketPolicyRequest(t, h, http.MethodPut, body)
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyOpenAICodexTicketEnabled])
	require.Contains(t, repo.values[service.SettingKeyCodexTicketPolicy], `"target_length":352`)
	get = ticketPolicyRequest(t, h, http.MethodGet, nil)
	require.Equal(t, put.Body.String(), get.Body.String())
	require.NotContains(t, get.Body.String(), "private-secret")
	// 系统设置保存其它项不重写本页规则或开关。
	rec := doUpdateSettings(t, h, map[string]any{"site_name": "updated"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "true", repo.values[service.SettingKeyOpenAICodexTicketEnabled])
	require.Contains(t, repo.values[service.SettingKeyCodexTicketPolicy], `"target_length":352`)
}

func TestCodexTicketPolicyHandlerRejectsBadPayload(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, nil)
	for _, body := range []string{`{}`, `null`, `{"unexpected":1}`, `{} {}`, `{"enabled":"true"}`} {
		put := ticketPolicyRequest(t, h, http.MethodPut, []byte(body))
		require.Equal(t, http.StatusBadRequest, put.Code, body)
		require.Empty(t, repo.values[service.SettingKeyCodexTicketPolicy])
	}
}

func TestCodexTicketLengthModeHandlerRoundTrip(t *testing.T) {
	h, repo := newStepUpSwitchTestHandler(t, nil)
	get := ticketPolicyRequest(t, h, http.MethodGet, nil)
	var initial struct {
		Data config.OpenAICodexTicketConfig `json:"data"`
	}
	require.NoError(t, json.Unmarshal(get.Body.Bytes(), &initial))
	initial.Data.LengthMode = config.CodexTicketLengthAuto
	body, err := json.Marshal(initial.Data)
	require.NoError(t, err)
	put := ticketPolicyRequest(t, h, http.MethodPut, body)
	require.Equal(t, http.StatusOK, put.Code, put.Body.String())
	require.Equal(t, put.Body.String(), ticketPolicyRequest(t, h, http.MethodGet, nil).Body.String())
	require.Contains(t, repo.values[service.SettingKeyCodexTicketPolicy], `"length_mode":"auto"`)
	before := repo.values[service.SettingKeyCodexTicketPolicy]
	initial.Data.LengthMode = "unknown"
	body, err = json.Marshal(initial.Data)
	require.NoError(t, err)
	put = ticketPolicyRequest(t, h, http.MethodPut, body)
	require.Equal(t, http.StatusBadRequest, put.Code, put.Body.String())
	require.Equal(t, before, repo.values[service.SettingKeyCodexTicketPolicy])
}
