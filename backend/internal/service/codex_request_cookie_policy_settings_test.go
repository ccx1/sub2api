package service

import (
	"context"
	"encoding/json"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexCookiePolicyLegacySettingsPreserve(t *testing.T) {
	for _, raw := range []string{
		"", `{"enabled":true,"strategy":"native","scope":"dedicated"}`,
		`{"enabled":true,"cookie_mode":""}`,
	} {
		t.Run(raw, func(t *testing.T) {
			repo := &modelQualitySettingsRepo{values: map[string]string{SettingKeyCodexRequestStrategy: raw}}
			svc := &SettingService{settingRepo: repo}
			loaded, err := svc.GetCodexRequestStrategyPolicy(context.Background())
			require.NoError(t, err)
			require.Equal(t, CodexCookiePreserve, loaded.CookieMode)
			require.Zero(t, repo.writes)
			require.Equal(t, raw, repo.values[SettingKeyCodexRequestStrategy])
		})
	}
	require.Equal(t, CodexCookiePreserve, DefaultCodexRequestStrategyPolicy().CookieMode)
}

func TestCodexCookiePolicySettingsRoundTrip(t *testing.T) {
	for _, mode := range []string{CodexCookiePreserve, CodexCookieStripRouting, CodexCookieStripCloudflare, CodexCookieStripInfrastructure} {
		t.Run(mode, func(t *testing.T) {
			repo := &modelQualitySettingsRepo{values: map[string]string{"unrelated_setting": "unchanged"}}
			svc := &SettingService{settingRepo: repo}
			policy := DefaultCodexRequestStrategyPolicy()
			policy.Enabled, policy.CookieMode = true, mode
			saved, err := svc.UpdateCodexRequestStrategyPolicy(context.Background(), policy)
			require.NoError(t, err)
			require.Equal(t, policy, saved)
			var stored map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyCodexRequestStrategy]), &stored))
			require.JSONEq(t, `"`+mode+`"`, string(stored["cookie_mode"]))
			loaded, err := (&SettingService{settingRepo: repo}).GetCodexRequestStrategyPolicy(context.Background())
			require.NoError(t, err)
			require.Equal(t, policy, loaded)
			require.Equal(t, 1, repo.writes)
			require.Equal(t, "unchanged", repo.values["unrelated_setting"])
		})
	}
}

func TestCodexCookiePolicyEmptyModeSavesPreserve(t *testing.T) {
	repo := &modelQualitySettingsRepo{}
	svc := &SettingService{settingRepo: repo}
	policy := DefaultCodexRequestStrategyPolicy()
	policy.CookieMode = ""
	saved, err := svc.UpdateCodexRequestStrategyPolicy(context.Background(), policy)
	require.NoError(t, err)
	require.Equal(t, CodexCookiePreserve, saved.CookieMode)
	loaded, err := svc.GetCodexRequestStrategyPolicy(context.Background())
	require.NoError(t, err)
	require.Equal(t, saved, loaded)
}

func TestCodexCookiePolicyRejectsInvalidSettingsBeforeWrite(t *testing.T) {
	for _, mode := range []string{"unknown", "PRESERVE", " ", " strip_routing "} {
		t.Run(mode, func(t *testing.T) {
			repo := &modelQualitySettingsRepo{values: map[string]string{SettingKeyCodexRequestStrategy: `{"cookie_mode":"preserve"}`}}
			before := maps.Clone(repo.values)
			svc := &SettingService{settingRepo: repo}
			policy := DefaultCodexRequestStrategyPolicy()
			policy.CookieMode = mode
			_, err := svc.UpdateCodexRequestStrategyPolicy(context.Background(), policy)
			require.ErrorContains(t, err, "cookie_mode must be")
			require.Zero(t, repo.writes)
			require.Equal(t, before, repo.values)
		})
	}
}

func TestCodexCookiePolicyStrictConflictNeverWrites(t *testing.T) {
	for _, mode := range []string{CodexCookieStripRouting, CodexCookieStripInfrastructure} {
		for _, enabled := range []bool{false, true} {
			policy := DefaultCodexRequestStrategyPolicy()
			policy.Enabled, policy.CookieMode, policy.RouteAffinityMode = enabled, mode, CodexRouteAffinityStrict
			repo := &modelQualitySettingsRepo{values: map[string]string{SettingKeyCodexRequestStrategy: `{"cookie_mode":"preserve"}`}}
			before := maps.Clone(repo.values)
			svc := &SettingService{settingRepo: repo}
			_, err := svc.UpdateCodexRequestStrategyPolicy(context.Background(), policy)
			require.ErrorContains(t, err, "strict route affinity is incompatible", "mode=%s enabled=%v", mode, enabled)
			require.Zero(t, repo.writes)
			require.Equal(t, before, repo.values)
		}
	}
}

func TestCodexCookiePolicyStrictAcceptsPreservedRoutes(t *testing.T) {
	for _, mode := range []string{CodexCookiePreserve, CodexCookieStripCloudflare} {
		policy := DefaultCodexRequestStrategyPolicy()
		policy.Enabled, policy.CookieMode, policy.RouteAffinityMode = true, mode, CodexRouteAffinityStrict
		repo := &modelQualitySettingsRepo{}
		svc := &SettingService{settingRepo: repo}
		_, err := svc.UpdateCodexRequestStrategyPolicy(context.Background(), policy)
		require.NoError(t, err)
		loaded, err := svc.GetCodexRequestStrategyPolicy(context.Background())
		require.NoError(t, err)
		require.Equal(t, policy, loaded)
	}
}

func TestCodexCookiePolicyStoredInvalidValueReportsError(t *testing.T) {
	for _, raw := range []string{
		`{"cookie_mode":"unknown"}`,
		`{"cookie_mode":"strip_routing","route_affinity_mode":"strict"}`,
	} {
		repo := &modelQualitySettingsRepo{values: map[string]string{SettingKeyCodexRequestStrategy: raw}}
		svc := &SettingService{settingRepo: repo}
		loaded, err := svc.GetCodexRequestStrategyPolicy(context.Background())
		require.Error(t, err)
		require.Equal(t, DefaultCodexRequestStrategyPolicy(), loaded)
		require.Zero(t, repo.writes)
		require.Equal(t, raw, repo.values[SettingKeyCodexRequestStrategy])
	}
}
