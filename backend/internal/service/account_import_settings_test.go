package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type accountImportSettingsRepoStub struct {
	SettingRepository
	raw      string
	readErr  error
	writeErr error
	writes   int
}

func (r *accountImportSettingsRepoStub) GetValue(_ context.Context, key string) (string, error) {
	if key == SettingKeyAccountProtectionDefaultMode {
		return string(AntiDegradeModeMinimal), nil
	}
	if r.readErr != nil {
		return "", r.readErr
	}
	if r.raw == "" {
		return "", ErrSettingNotFound
	}
	return r.raw, nil
}

func (r *accountImportSettingsRepoStub) Set(_ context.Context, key, value string) error {
	if key != SettingKeyAccountImportSettings {
		return errors.New("unexpected setting key")
	}
	if r.writeErr != nil {
		return r.writeErr
	}
	r.raw, r.writes = value, r.writes+1
	return nil
}

type accountImportProxyRepoStub struct {
	ProxyRepository
	ProxyGroupRepository
	proxy  *Proxy
	group  int64
	err    error
	looked []int64
}

func (r *accountImportProxyRepoStub) GetByID(_ context.Context, id int64) (*Proxy, error) {
	r.looked = append(r.looked, id)
	return r.proxy, r.err
}

func (r *accountImportProxyRepoStub) ProxyGroupExists(_ context.Context, id int64) (bool, error) {
	return id == r.group, r.err
}

func accountImportSettingsService(t *testing.T, settings AccountImportSettings) (*SettingService, *accountImportSettingsRepoStub) {
	t.Helper()
	raw, err := json.Marshal(settings)
	require.NoError(t, err)
	repo := &accountImportSettingsRepoStub{raw: string(raw)}
	return &SettingService{settingRepo: repo}, repo
}

func TestAccountImportSettingsDefaultAndRoundTrip(t *testing.T) {
	ctx := context.Background()
	repo := &accountImportSettingsRepoStub{}
	svc := &SettingService{settingRepo: repo}
	settings, err := svc.GetAccountImportSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, DefaultAccountImportSettings(), *settings)
	settings.Enabled = true
	settings.CodexTicketEnabled = false
	settings.ProxyMode = "random"
	settings.Extra = map[string]any{
		RandomProxyPoolScopeExtraKey:       RandomProxyPoolAll,
		RandomProxyEmptyPoolPolicyExtraKey: RandomProxyEmptyPoolPolicyReject,
		ProxyRegionModeExtraKey:            "billing",
		ProxyRegionFallbackCountryExtraKey: "jp",
		CodexTicketProxyModeExtraKey:       CodexTicketProxyModeAccount,
	}
	saved, err := svc.UpdateAccountImportSettings(ctx, *settings)
	require.NoError(t, err)
	require.Equal(t, "jp", settings.Extra[ProxyRegionFallbackCountryExtraKey])
	require.Equal(t, "JP", saved.Extra[ProxyRegionFallbackCountryExtraKey])
	reread, err := svc.GetAccountImportSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, saved, reread)
	require.Equal(t, 1, repo.writes)
}

func TestAccountImportSettingsRejectsInvalidSavedOrRequestedValues(t *testing.T) {
	ctx := context.Background()
	for _, raw := range []string{
		"broken", "null", `{"enabled":true,"proxy_mode":"other"}`,
		`{"enabled":true,"extra":{"credentials":{"token":"untrusted"}}}`,
		`{"enabled":true,"extra":{"random_proxy_empty_pool_policy":"ignore"}}`,
		`{"enabled":true,"proxy_mode":"fixed","proxy_id":0}`,
		`{"enabled":true,"extra":{"random_proxy_pool_scope":"selected","random_proxy_pool_ids":[]}}`,
		`{"enabled":true,"extra":{"proxy_region_mode":"manual"}}`,
		`{"enabled":true,"extra":{"proxy_region_mode":"billing","proxy_region_fallback_country":"not-a-country"}}`,
		`{"enabled":true,"extra":{"codex_ticket_proxy_mode":"fixed"}}`,
		`{"unexpected":true}`, `{"enabled":true} {}`,
	} {
		t.Run(raw, func(t *testing.T) {
			svc := &SettingService{settingRepo: &accountImportSettingsRepoStub{raw: raw}}
			_, err := svc.GetAccountImportSettings(ctx)
			require.Error(t, err)
		})
	}
	svc := &SettingService{settingRepo: &accountImportSettingsRepoStub{}}
	settings := DefaultAccountImportSettings()
	settings.Extra["credentials"] = map[string]any{"access_token": "untrusted"}
	_, err := svc.UpdateAccountImportSettings(ctx, settings)
	require.Error(t, err)
}

func TestAccountImportSettingsRepositoryFailures(t *testing.T) {
	ctx := context.Background()
	failure := errors.New("settings unavailable")
	repo := &accountImportSettingsRepoStub{readErr: failure}
	svc := &SettingService{settingRepo: repo}
	_, err := svc.GetAccountImportSettings(ctx)
	require.ErrorIs(t, err, failure)
	repo.readErr, repo.writeErr = nil, failure
	_, err = svc.UpdateAccountImportSettings(ctx, DefaultAccountImportSettings())
	require.ErrorIs(t, err, failure)
	require.Zero(t, repo.writes)
}

func TestAccountImportSettingsValidatesFixedAndTicketProxies(t *testing.T) {
	ctx := context.Background()
	settings := DefaultAccountImportSettings()
	settings.Enabled, settings.ProxyMode = true, "fixed"
	id := int64(7)
	settings.ProxyID = &id
	proxy := &Proxy{ID: id, Protocol: "http", Host: "127.0.0.1", Port: 8080, Status: StatusActive}
	proxyRepo := &accountImportProxyRepoStub{proxy: proxy}
	svc := &SettingService{settingRepo: &accountImportSettingsRepoStub{}, proxyRepo: proxyRepo}
	_, err := svc.UpdateAccountImportSettings(ctx, settings)
	require.NoError(t, err)
	for _, change := range []func(){
		func() { proxy.Status = StatusDisabled },
		func() { proxy.Status = StatusActive; past := time.Now().Add(-time.Minute); proxy.ExpiresAt = &past },
		func() { proxy.ExpiresAt = nil; proxy.ID = 8 },
		func() { proxyRepo.proxy = nil },
	} {
		change()
		_, err = svc.UpdateAccountImportSettings(ctx, settings)
		require.Error(t, err)
	}
	settings.ProxyMode, settings.ProxyID = "preserve", nil
	settings.Extra = map[string]any{CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: id}
	_, err = svc.UpdateAccountImportSettings(ctx, settings)
	require.Error(t, err)
	settings.Enabled = false
	_, err = svc.UpdateAccountImportSettings(ctx, settings)
	require.NoError(t, err, "失效代理不能阻止停用默认设置")
}

func TestAccountImportSettingsValidatesRandomProxyGroup(t *testing.T) {
	settings := DefaultAccountImportSettings()
	settings.Enabled, settings.ProxyMode = true, "random"
	settings.Extra = map[string]any{RandomProxyPoolScopeExtraKey: "group", RandomProxyGroupIDExtraKey: int64(5)}
	proxyRepo := &accountImportProxyRepoStub{group: 5}
	svc := &SettingService{settingRepo: &accountImportSettingsRepoStub{}, proxyRepo: proxyRepo}
	_, err := svc.UpdateAccountImportSettings(context.Background(), settings)
	require.NoError(t, err)
	proxyRepo.group = 0
	_, err = svc.UpdateAccountImportSettings(context.Background(), settings)
	require.Error(t, err)
	require.Equal(t, int64(5), settings.Extra[RandomProxyGroupIDExtraKey])
}
