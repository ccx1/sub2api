package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func readProxyPoolSettings(t *testing.T, h *SettingHandler) dto.SystemSettings {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings", nil)
	h.GetSettings(c)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var result struct {
		Data dto.SystemSettings `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
	return result.Data
}

func TestSettingsProxyPoolWriteReadAndHotReload(t *testing.T) {
	modeKey, maxKey := service.SettingKeyOpenAICodexTicketHarvestProxyMode, service.SettingKeyProxyPoolMaxAccounts
	proxyKey := service.SettingKeyOpenAICodexTicketHarvestProxyURL
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{proxyKey: "http://user:private-secret@proxy.example.com:8080"})
	ctx := context.Background()
	mode, err := h.settingService.GetOpenAICodexTicketHarvestProxyMode(ctx)
	require.NoError(t, err)
	require.Equal(t, "fixed", mode)
	max, err := h.settingService.GetProxyPoolMaxAccounts(ctx)
	require.NoError(t, err)
	require.Zero(t, max)
	for _, limit := range []int{4, 10000, 0} {
		rec := doUpdateSettings(t, h, map[string]any{modeKey: "pool", maxKey: limit}, nil)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.NotContains(t, rec.Body.String(), "private-secret")
		mode, err = h.settingService.GetOpenAICodexTicketHarvestProxyMode(ctx)
		require.NoError(t, err)
		require.Equal(t, "pool", mode)
		max, err = h.settingService.GetProxyPoolMaxAccounts(ctx)
		require.NoError(t, err)
		require.Equal(t, limit, max)
		settings := readProxyPoolSettings(t, h)
		require.Equal(t, "pool", settings.OpenAICodexTicketHarvestProxyMode)
		require.Equal(t, limit, settings.ProxyPoolMaxAccounts)
		require.NotContains(t, settings.OpenAICodexTicketHarvestProxyURL, "private-secret")
	}
	require.Equal(t, "pool", repo.values[modeKey])
	require.Equal(t, "http://user:private-secret@proxy.example.com:8080", repo.values[proxyKey])
}

func TestSettingsProxyPoolOmissionPreservesStoredValues(t *testing.T) {
	modeKey, maxKey := service.SettingKeyOpenAICodexTicketHarvestProxyMode, service.SettingKeyProxyPoolMaxAccounts
	h, repo := newStepUpSwitchTestHandler(t, map[string]string{modeKey: "pool", maxKey: "3"})
	rec := doUpdateSettings(t, h, map[string]any{"site_name": "updated"}, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "pool", repo.values[modeKey])
	require.Equal(t, "3", repo.values[maxKey])
	require.NotContains(t, repo.lastUpdates, modeKey)
	require.NotContains(t, repo.lastUpdates, maxKey)
	settings := readProxyPoolSettings(t, h)
	require.Equal(t, "pool", settings.OpenAICodexTicketHarvestProxyMode)
	require.Equal(t, 3, settings.ProxyPoolMaxAccounts)
}

func TestSettingsProxyPoolLegacyModeCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, storedURL, yamlURL, explicit, want string
	}{
		{name: "new", want: "pool"},
		{name: "legacy stored", storedURL: "http://old.example.com:8080", want: "fixed"},
		{name: "legacy yaml", yamlURL: "http://yaml.example.com:8080", want: "fixed"},
		{name: "explicit pool", storedURL: "http://old.example.com:8080", yamlURL: "http://yaml.example.com:8080", explicit: "pool", want: "pool"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &settingHandlerRepoStub{values: map[string]string{service.SettingKeyOpenAICodexTicketHarvestProxyURL: tc.storedURL}}
			if tc.explicit != "" {
				repo.values[service.SettingKeyOpenAICodexTicketHarvestProxyMode] = tc.explicit
			}
			cfg := &config.Config{}
			cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = tc.yamlURL
			h := NewSettingHandler(service.NewSettingService(repo, cfg), nil, nil, nil, nil, nil, nil)
			require.Equal(t, tc.want, readProxyPoolSettings(t, h).OpenAICodexTicketHarvestProxyMode)
			rec := doUpdateSettings(t, h, map[string]any{"site_name": "updated"}, nil)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			mode, err := h.settingService.GetOpenAICodexTicketHarvestProxyMode(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.want, mode)
			if tc.explicit == "" {
				require.NotContains(t, repo.values, service.SettingKeyOpenAICodexTicketHarvestProxyMode)
			}
		})
	}
}

func TestSettingsProxyPoolRejectInvalidValuesWithoutWriting(t *testing.T) {
	modeKey, maxKey := service.SettingKeyOpenAICodexTicketHarvestProxyMode, service.SettingKeyProxyPoolMaxAccounts
	for _, body := range []map[string]any{
		{modeKey: "direct"}, {modeKey: ""}, {modeKey: nil}, {maxKey: -1}, {maxKey: 10001}, {maxKey: 1.5}, {maxKey: nil},
	} {
		h, repo := newStepUpSwitchTestHandler(t, map[string]string{modeKey: "fixed", maxKey: "3"})
		rec := doUpdateSettings(t, h, body, nil)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Equal(t, "fixed", repo.values[modeKey])
		require.Equal(t, "3", repo.values[maxKey])
		require.Nil(t, repo.lastUpdates)
	}
}

func TestSettingsProxyPoolAuditIncludesChangedFields(t *testing.T) {
	before := &service.SystemSettings{OpenAICodexTicketHarvestProxyMode: "fixed"}
	after := &service.SystemSettings{OpenAICodexTicketHarvestProxyMode: "pool", ProxyPoolMaxAccounts: 3}
	changed := diffSettings(before, after, nil, nil, UpdateSettingsRequest{})
	require.Contains(t, changed, service.SettingKeyOpenAICodexTicketHarvestProxyMode)
	require.Contains(t, changed, service.SettingKeyProxyPoolMaxAccounts)
}
