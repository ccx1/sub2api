package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type astraHandlerGroupRepo struct {
	astraHandlerRepo
	seen config.AstraRoutingSettings
}

func (r *astraHandlerGroupRepo) ResolveAstraRoutingAccounts(_ context.Context, value config.AstraRoutingSettings) (config.AstraRoutingSettings, error) {
	r.seen = value
	value.CookiePool.SourceAccountIDs = []int64{10}
	return config.ResolveAstraDependencies(value)
}

func TestAstraGroupHandlerResolvesDonorBeforeWSExpansion(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &astraHandlerGroupRepo{}
	cfg := &config.Config{}
	cfg.Gateway.OpenAIWS = config.GatewayOpenAIWSConfig{Enabled: true, OAuthEnabled: true, ResponsesWebsocketsV2: true}
	h := &SettingHandler{settingService: service.NewSettingService(repo, cfg)}
	router := gin.New()
	router.PUT("/settings", h.UpdateAstraRouting)
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(`{"cookie_pool":{"source_selection":"groups","source_group_ids":[1],"source_account_ids":[999],"target_selection":"accounts","target_account_ids":[]},"ws_session":{"enabled":true,"account_ids":[10,30]}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Empty(t, repo.seen.CookiePool.SourceAccountIDs)
	var stored config.AstraRoutingSettings
	require.NoError(t, json.Unmarshal([]byte(repo.value), &stored))
	require.Empty(t, stored.CookiePool.SourceAccountIDs)
	require.Equal(t, []int64{30}, stored.CookiePool.TargetAccountIDs)
}

type astraHandlerRepo struct {
	service.SettingRepository
	value string
}

func (r *astraHandlerRepo) GetValue(context.Context, string) (string, error) {
	if r.value == "" {
		return "", service.ErrSettingNotFound
	}
	return r.value, nil
}
func (r *astraHandlerRepo) Set(_ context.Context, _ string, value string) error {
	r.value = value
	return nil
}
func TestAstraGatewayHandlerRoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &astraHandlerRepo{}
	cfg := &config.Config{}
	h := &SettingHandler{settingService: service.NewSettingService(repo, cfg)}
	router := gin.New()
	router.GET("/settings", h.GetAstraRouting)
	router.PUT("/settings", h.UpdateAstraRouting)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/settings", nil))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"enabled":false`)
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(`{"cookie_pool":{"enabled":true,"source_account_ids":[299],"target_account_ids":[300]},"ws_session":{"enabled":false,"account_ids":[]}}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)
	require.True(t, cfg.AstraRouting(t.Context()).CookiePool.Enabled)
	before := repo.value
	req = httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(`{"cookie_pool":{"enabled":true,"source_account_ids":[299],"target_account_ids":[299]}}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, 400, w.Code)
	require.Equal(t, before, repo.value)
}

func TestAstraGatewayHandlerAutomaticallyExcludesSourceFromTargets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &astraHandlerRepo{}
	h := &SettingHandler{settingService: service.NewSettingService(repo, &config.Config{})}
	router := gin.New()
	router.PUT("/settings", h.UpdateAstraRouting)
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(`{"cookie_pool":{"enabled":true,"source_account_ids":[299],"target_account_ids":[299,300]}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var stored config.AstraRoutingSettings
	require.NoError(t, json.Unmarshal([]byte(repo.value), &stored))
	require.Equal(t, []int64{299}, stored.CookiePool.SourceAccountIDs)
	require.Equal(t, []int64{300}, stored.CookiePool.TargetAccountIDs)
}
