package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountImportSettingsStore struct {
	service.SettingRepository
	value             string
	readErr, writeErr error
}

func (s *accountImportSettingsStore) GetValue(context.Context, string) (string, error) {
	if s.readErr != nil {
		return "", s.readErr
	}
	if s.value == "" {
		return "", service.ErrSettingNotFound
	}
	return s.value, nil
}

func (s *accountImportSettingsStore) Set(_ context.Context, _, value string) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	s.value = value
	return nil
}

func serveImportSettings(t *testing.T, h *SettingHandler, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/settings/account-import", h.GetAccountImportSettings)
	router.PUT("/settings/account-import", h.UpdateAccountImportSettings)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/settings/account-import", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}

func TestAccountImportSettingsRoundTripAndValidation(t *testing.T) {
	repo := &accountImportSettingsStore{}
	h := &SettingHandler{settingService: service.NewSettingService(repo, &config.Config{})}
	rec := serveImportSettings(t, h, http.MethodGet, "")
	require.Equal(t, http.StatusOK, rec.Code)
	var result struct {
		Data service.AccountImportSettings `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.False(t, result.Data.Enabled)

	valid := `{"enabled":true,"protection_enabled":false,"codex_ticket_enabled":false,"proxy_mode":"random","proxy_id":null,"extra":{"random_proxy_pool_scope":"all","random_proxy_empty_pool_policy":"reject","proxy_region_mode":"manual","proxy_region_country":"JP","codex_ticket_proxy_mode":"account"}}`
	rec = serveImportSettings(t, h, http.MethodPut, valid)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotEmpty(t, repo.value)
	rec = serveImportSettings(t, h, http.MethodGet, "")
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.True(t, result.Data.Enabled)
	require.False(t, result.Data.ProtectionEnabled)
	require.False(t, result.Data.CodexTicketEnabled)
	require.Equal(t, "random", result.Data.ProxyMode)
	require.Equal(t, "JP", result.Data.Extra["proxy_region_country"])
	require.False(t, result.Data.ExcelBPSEnabled, "旧请求缺少 excel_bps_enabled 时按关闭保存")

	rec = serveImportSettings(t, h, http.MethodPut, `{"enabled":true,"protection_enabled":true,"codex_ticket_enabled":true,"excel_bps_enabled":true,"proxy_mode":"preserve","proxy_id":null,"extra":{}}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = serveImportSettings(t, h, http.MethodGet, "")
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	require.True(t, result.Data.ExcelBPSEnabled)

	for _, invalid := range []string{
		`{}`, `{"enabled":"true"}`,
		`{"enabled":true,"protection_enabled":true,"codex_ticket_enabled":true,"proxy_mode":"unknown"}`,
		`{"enabled":true,"protection_enabled":true,"codex_ticket_enabled":true,"proxy_mode":"fixed"}`,
		`{"enabled":true,"protection_enabled":true,"codex_ticket_enabled":true,"proxy_mode":"preserve","extra":{"access_token":"forbidden"}}`,
	} {
		before := repo.value
		rec = serveImportSettings(t, h, http.MethodPut, invalid)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Equal(t, before, repo.value)
	}
}

func TestAccountImportSettingsReportsStorageFailure(t *testing.T) {
	repo := &accountImportSettingsStore{readErr: errors.New("storage unavailable")}
	h := &SettingHandler{settingService: service.NewSettingService(repo, &config.Config{})}
	rec := serveImportSettings(t, h, http.MethodGet, "")
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	repo.readErr = nil
	repo.writeErr = errors.New("write unavailable")
	rec = serveImportSettings(t, h, http.MethodPut, `{"enabled":true,"protection_enabled":true,"codex_ticket_enabled":false,"proxy_mode":"preserve","extra":{}}`)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Empty(t, repo.value)
	rec = serveImportSettings(t, &SettingHandler{}, http.MethodGet, "")
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
