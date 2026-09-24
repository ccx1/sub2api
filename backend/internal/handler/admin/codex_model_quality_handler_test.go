package admin

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type qualityHandlerSettingsRepo struct {
	*settingHandlerRepoStub
	readErr, writeErr error
}

func (r *qualityHandlerSettingsRepo) GetValue(ctx context.Context, key string) (string, error) {
	if r.readErr != nil {
		return "", r.readErr
	}
	return r.settingHandlerRepoStub.GetValue(ctx, key)
}

func (r *qualityHandlerSettingsRepo) SetMultiple(ctx context.Context, values map[string]string) error {
	if r.writeErr != nil {
		return r.writeErr
	}
	return r.settingHandlerRepoStub.SetMultiple(ctx, values)
}

func qualityPolicyRequest(h *SettingHandler, method, body string) *httptest.ResponseRecorder {
	router := gin.New()
	router.GET("/settings/codex-model-quality", h.GetCodexModelQualityPolicy)
	router.PUT("/settings/codex-model-quality", h.UpdateCodexModelQualityPolicy)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(method, "/settings/codex-model-quality", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestCodexModelQualityPolicyHandlerRoundTrip(t *testing.T) {
	repo := &settingHandlerRepoStub{values: map[string]string{service.SettingKeyCodexTicketPolicy: "unchanged"}}
	h := &SettingHandler{settingService: service.NewSettingService(repo, &config.Config{})}
	initial := qualityPolicyRequest(h, http.MethodGet, "")
	require.Equal(t, http.StatusOK, initial.Code)
	var envelope struct {
		Data service.CodexModelQualityPolicy `json:"data"`
	}
	require.NoError(t, json.Unmarshal(initial.Body.Bytes(), &envelope))
	require.False(t, envelope.Data.Enabled)
	envelope.Data.Enabled, envelope.Data.FingerprintEnabled = true, false
	envelope.Data.TimeoutSeconds, envelope.Data.ReasoningEffort = 25, "high"
	body, err := json.Marshal(envelope.Data)
	require.NoError(t, err)
	updated := qualityPolicyRequest(h, http.MethodPut, string(body))
	require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
	loaded := qualityPolicyRequest(h, http.MethodGet, "")
	require.Equal(t, http.StatusOK, loaded.Code)
	require.JSONEq(t, updated.Body.String(), loaded.Body.String())
	require.NoError(t, json.Unmarshal(loaded.Body.Bytes(), &envelope))
	require.True(t, envelope.Data.Enabled)
	require.False(t, envelope.Data.FingerprintEnabled)
	require.Equal(t, 25, envelope.Data.TimeoutSeconds)
	require.Equal(t, "high", envelope.Data.ReasoningEffort)
	require.Equal(t, "unchanged", repo.values[service.SettingKeyCodexTicketPolicy])
	require.Len(t, repo.lastUpdates, 1)
	require.Contains(t, repo.lastUpdates, service.SettingKeyCodexModelQuality)
}

func TestCodexModelQualityPolicyHandlerRejectsInvalidJSON(t *testing.T) {
	repo := &settingHandlerRepoStub{values: map[string]string{service.SettingKeyCodexModelQuality: `{"enabled":true}`}}
	h := &SettingHandler{settingService: service.NewSettingService(repo, &config.Config{})}
	for _, body := range []string{
		"", "null", "[]", `{"unknown":true}`, `{"enabled":true} {}`, `{} null`,
		`{"enabled":"true"}`, `{"timeout_seconds":4}`, `{"reasoning_effort":"xhigh"}`,
		`{"enabled":true`, `{"enabled":true}` + strings.Repeat(" ", 4096),
	} {
		before := maps.Clone(repo.values)
		recorder := qualityPolicyRequest(h, http.MethodPut, body)
		require.Equal(t, http.StatusBadRequest, recorder.Code, body)
		require.Equal(t, before, repo.values)
		require.Empty(t, repo.lastUpdates)
	}
}

func TestCodexModelQualityPolicyHandlerStorageFailures(t *testing.T) {
	repo := &qualityHandlerSettingsRepo{settingHandlerRepoStub: &settingHandlerRepoStub{values: map[string]string{}}}
	h := &SettingHandler{settingService: service.NewSettingService(repo, &config.Config{})}
	repo.readErr = errors.New("private-storage-marker")
	read := qualityPolicyRequest(h, http.MethodGet, "")
	require.Equal(t, http.StatusInternalServerError, read.Code)
	require.NotContains(t, read.Body.String(), "private-storage-marker")
	repo.readErr, repo.writeErr = nil, errors.New("write failed")
	write := qualityPolicyRequest(h, http.MethodPut, `{"enabled":true}`)
	require.Equal(t, http.StatusInternalServerError, write.Code)
	require.Empty(t, repo.values)
	repo.writeErr = nil
	for _, raw := range []string{"null", "malformed-private-marker", `{"timeout_seconds":0}`} {
		repo.values[service.SettingKeyCodexModelQuality] = raw
		read = qualityPolicyRequest(h, http.MethodGet, "")
		require.GreaterOrEqual(t, read.Code, http.StatusBadRequest)
		require.NotContains(t, read.Body.String(), "malformed-private-marker")
	}
}

func TestCodexModelQualityPolicyHandlerMissingService(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		require.NotPanics(t, func() {
			recorder := qualityPolicyRequest(&SettingHandler{}, method, `{}`)
			require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
		})
	}
}

type qualityAccountHTTPRequest struct {
	method, id, body string
}

func qualityAccountRequest(h *AccountHandler, request qualityAccountHTTPRequest) *httptest.ResponseRecorder {
	router := gin.New()
	router.GET("/accounts/:id/codex-model-quality", h.GetCodexModelQuality)
	router.POST("/accounts/:id/codex-model-quality/retry", h.RetryCodexModelQuality)
	url := "/accounts/" + request.id + "/codex-model-quality"
	if request.method == http.MethodPost {
		url += "/retry"
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(request.method, url, strings.NewReader(request.body)))
	return recorder
}

func TestCodexModelQualityAccountHandlerRejectsInvalidID(t *testing.T) {
	for _, id := range []string{"0", "-1", "bad", "9223372036854775808"} {
		stub := &ticketHistoryAdminStub{account: ticketHistoryAccount()}
		h := &AccountHandler{adminService: stub}
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			recorder := qualityAccountRequest(h, qualityAccountHTTPRequest{method, id, `{"model":"gpt-6-astra"}`})
			require.Equal(t, http.StatusBadRequest, recorder.Code, id)
		}
		require.Empty(t, stub.calls)
	}
}

func TestCodexModelQualityAccountHandlerRejectsUnsupportedAccounts(t *testing.T) {
	parentID := int64(1)
	for _, account := range []*service.Account{
		nil,
		{ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey},
		{ID: 41, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth},
		{ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, ParentAccountID: &parentID},
	} {
		h := &AccountHandler{adminService: &ticketHistoryAdminStub{account: account}}
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			recorder := qualityAccountRequest(h, qualityAccountHTTPRequest{method, "41", `{"model":"gpt-6-astra"}`})
			require.Equal(t, http.StatusBadRequest, recorder.Code)
		}
	}
}

func TestCodexModelQualityAccountHandlerPropagatesLookupErrors(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{service.ErrAccountNotFound, http.StatusNotFound},
		{errors.New("lookup failed"), http.StatusInternalServerError},
	} {
		h := &AccountHandler{adminService: &ticketHistoryAdminStub{err: test.err}}
		recorder := qualityAccountRequest(h, qualityAccountHTTPRequest{http.MethodGet, "41", ""})
		require.Equal(t, test.status, recorder.Code)
	}
}

func TestCodexModelQualityRetryHandlerRejectsInvalidModel(t *testing.T) {
	h := &AccountHandler{adminService: &ticketHistoryAdminStub{account: ticketHistoryAccount()}}
	for _, body := range []string{
		`{}`, `null`, `{"model":""}`, `{"model":"  "}`, `{"model":17}`,
		`{"model":"` + strings.Repeat("x", 161) + `"}`,
		`{"model":"gpt-6-astra","unknown":true}`, `{"model":"gpt-6-astra"} {}`,
	} {
		recorder := qualityAccountRequest(h, qualityAccountHTTPRequest{http.MethodPost, "41", body})
		require.Equal(t, http.StatusBadRequest, recorder.Code, body)
	}
}

func TestCodexModelQualityAccountHandlerMissingService(t *testing.T) {
	for _, h := range []*AccountHandler{{}, {adminService: &ticketHistoryAdminStub{account: ticketHistoryAccount()}}} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			require.NotPanics(t, func() {
				recorder := qualityAccountRequest(h, qualityAccountHTTPRequest{method, "41", `{"model":"gpt-6-astra"}`})
				require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
			})
		}
	}
}
