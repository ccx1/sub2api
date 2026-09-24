package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexRequestStrategyPolicyDefaultsDisabled(t *testing.T) {
	policy := DefaultCodexRequestStrategyPolicy()
	if policy.Enabled {
		t.Fatal("request strategy must be disabled by default")
	}
	if policy.RegionMode != CodexRequestRegionStrip || policy.TimeContextMode != CodexRequestTimeContextStrip || policy.ComplianceMode != CodexRequestComplianceAccount {
		t.Fatalf("unexpected context defaults: region=%s time=%s compliance=%s", policy.RegionMode, policy.TimeContextMode, policy.ComplianceMode)
	}
	if err := ValidateCodexRequestStrategyPolicy(policy); err != nil {
		t.Fatalf("default policy should validate: %v", err)
	}
}

func TestValidateCodexRequestStrategyPolicy(t *testing.T) {
	valid := DefaultCodexRequestStrategyPolicy()
	valid.Enabled = true
	valid.Strategy = CodexRequestStrategyCookiePreviousWS
	for name, mutate := range map[string]func(*CodexRequestStrategyPolicy){
		"strategy":       func(p *CodexRequestStrategyPolicy) { p.Strategy = "unknown" },
		"scope":          func(p *CodexRequestStrategyPolicy) { p.Scope = "unknown" },
		"failure":        func(p *CodexRequestStrategyPolicy) { p.FailureMode = "unknown" },
		"timeout":        func(p *CodexRequestStrategyPolicy) { p.ProbeTimeoutSeconds = 4 },
		"route_mode":     func(p *CodexRequestStrategyPolicy) { p.RouteAffinityMode = "unknown" },
		"route_prewarm":  func(p *CodexRequestStrategyPolicy) { p.RoutePrewarmConnections = 13 },
		"route_cooldown": func(p *CodexRequestStrategyPolicy) { p.RouteFailureCooldownSeconds = 29 },
	} {
		t.Run(name, func(t *testing.T) {
			policy := valid
			mutate(&policy)
			if err := ValidateCodexRequestStrategyPolicy(policy); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestCodexRequestStrategyPolicyPersistsZeroRoutePrewarm(t *testing.T) {
	repo := &modelQualitySettingsRepo{}
	svc := &SettingService{settingRepo: repo}
	policy := DefaultCodexRequestStrategyPolicy()
	policy.Enabled, policy.RouteAffinityMode = true, CodexRouteAffinityStrict
	policy.RoutePrewarmConnections = 0
	saved, err := svc.UpdateCodexRequestStrategyPolicy(context.Background(), policy)
	require.NoError(t, err)
	require.Equal(t, policy, saved)
	var stored map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(repo.values[SettingKeyCodexRequestStrategy]), &stored))
	require.JSONEq(t, "0", string(stored["route_prewarm_connections"]), "显式关闭预热不能在序列化时丢失")
	restarted := &SettingService{settingRepo: repo}
	loaded, err := restarted.GetCodexRequestStrategyPolicy(context.Background())
	require.NoError(t, err)
	require.Equal(t, policy, loaded)
	require.Zero(t, loaded.RoutePrewarmConnections)
}

func TestCodexRequestStrategyPolicyLegacyJSONDefaultsRoutePrewarm(t *testing.T) {
	repo := &modelQualitySettingsRepo{values: map[string]string{
		SettingKeyCodexRequestStrategy: `{"enabled":true,"strategy":"native","scope":"dedicated"}`,
	}}
	svc := &SettingService{settingRepo: repo}
	loaded, err := svc.GetCodexRequestStrategyPolicy(context.Background())
	require.NoError(t, err)
	require.True(t, loaded.Enabled)
	require.Equal(t, 4, loaded.RoutePrewarmConnections)
	require.Zero(t, repo.writes, "读取旧配置不能隐式写入数据库")
}
