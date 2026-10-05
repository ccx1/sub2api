package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type prismSettingsRepo struct {
	SettingRepository
	values map[string]string
	err    error
}

func (r *prismSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make(map[string]string)
	for _, key := range keys {
		if value, exists := r.values[key]; exists {
			out[key] = value
		}
	}
	return out, nil
}

func prismSettingsConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Gateway.PrismBrowser.Enabled = true
	cfg.Gateway.PrismBrowser.BaseURL = "http://127.0.0.1:8319/v1"
	cfg.Gateway.PrismBrowser.APIKey = strings.Repeat("x", 32)
	return cfg
}

func TestPrismBrowserSettingsFallbackAndIndependentOverrides(t *testing.T) {
	repo := &prismSettingsRepo{values: map[string]string{}}
	svc := &SettingService{settingRepo: repo, cfg: prismSettingsConfig()}
	ctx := context.Background()
	initial := svc.GetPrismBrowserRuntime(ctx)
	require.True(t, initial.Enabled, "existing cfg/env opt-in survives missing database keys")
	require.Equal(t, "http://127.0.0.1:8319/v1", initial.BaseURL)
	require.Equal(t, strings.Repeat("x", 32), initial.APIKey)
	repo.values[SettingKeyPrismBrowserEnabled] = "false"
	repo.values[SettingKeyPrismBrowserBaseURL] = "http://[::1]:9321/v1"
	next := svc.GetPrismBrowserRuntime(ctx)
	require.False(t, next.Enabled)
	require.Equal(t, "http://[::1]:9321/v1", next.BaseURL)
	require.Equal(t, initial.APIKey, next.APIKey, "one override cannot discard another cfg value")
	repo.values[SettingKeyPrismBrowserAPIKey] = ""
	require.Empty(t, svc.GetPrismBrowserRuntime(ctx).APIKey, "present empty values do not revive cfg credentials")
	repo.err = errors.New("database unavailable")
	require.False(t, svc.GetPrismBrowserRuntime(ctx).Enabled)
	require.Empty(t, svc.GetPrismBrowserRuntime(ctx).APIKey)
	defaultSvc := &SettingService{settingRepo: &prismSettingsRepo{}}
	require.False(t, defaultSvc.GetPrismBrowserRuntime(ctx).Enabled)
}

func TestPrismBrowserSettingsValidateLoopbackAndRedactSecret(t *testing.T) {
	valid := configuredPrismBrowserRuntime(prismSettingsConfig())
	require.NoError(t, ValidatePrismBrowserRuntime(valid))
	for _, address := range []string{"http://localhost:8319/v1", "https://127.0.0.1:8319/v1", "http://192.0.2.1:8319/v1", "http://127.0.0.1:8319/v1?key=bad", "http://127.0.0.1:8319/other"} {
		input := valid
		input.BaseURL = address
		require.Error(t, ValidatePrismBrowserRuntime(input), address)
	}
	invalid := valid
	invalid.APIKey = "bad\r\nheader"
	require.Error(t, ValidatePrismBrowserRuntime(invalid))
	invalid.APIKey = ""
	require.Error(t, ValidatePrismBrowserRuntime(invalid))
	svc := &SettingService{cfg: prismSettingsConfig()}
	settings := svc.parseSettings(map[string]string{})
	require.True(t, settings.PrismBrowserAPIKeyConfigured)
	data, err := json.Marshal(settings)
	require.NoError(t, err)
	require.NotContains(t, string(data), valid.APIKey)
}
