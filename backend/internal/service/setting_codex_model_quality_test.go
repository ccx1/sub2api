package service

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

type modelQualitySettingsRepo struct {
	SettingRepository
	values            map[string]string
	readErr, writeErr error
	writes            int
}

func (r *modelQualitySettingsRepo) GetValue(_ context.Context, key string) (string, error) {
	if r.readErr != nil {
		return "", r.readErr
	}
	if value, exists := r.values[key]; exists {
		return value, nil
	}
	return "", ErrSettingNotFound
}

func (r *modelQualitySettingsRepo) SetMultiple(_ context.Context, values map[string]string) error {
	if r.writeErr != nil {
		return r.writeErr
	}
	if r.values == nil {
		r.values = make(map[string]string)
	}
	maps.Copy(r.values, values)
	r.writes++
	return nil
}

func TestCodexModelQualityPolicyDefaultsAndIndependentRoundTrip(t *testing.T) {
	repo := &modelQualitySettingsRepo{values: map[string]string{
		SettingKeyCodexTicketPolicy:                `{"enabled":true,"models":["gpt-6-astra"]}`,
		SettingKeyOpenAICodexTicketEnabled:         "true",
		SettingKeyOpenAICodexTicketHarvestProxyURL: "http://private-proxy.test:8080",
	}}
	before := maps.Clone(repo.values)
	svc := &SettingService{settingRepo: repo}
	policy, err := svc.GetCodexModelQualityPolicy(context.Background())
	require.NoError(t, err)
	require.False(t, policy.Enabled)
	require.Equal(t, 3, policy.LowQualityConsecutiveThreshold)
	require.Equal(t, 900, policy.LowQualityCooldownSeconds)
	require.Equal(t, 300, policy.ReplacementCheckDelaySeconds)
	require.Equal(t, before, repo.values)
	require.Zero(t, repo.writes)
	policy.Enabled, policy.TimeoutSeconds, policy.ReasoningEffort = true, 45, "medium"
	policy.QuarantineOnFailure, policy.FingerprintEnabled = false, false
	saved, err := svc.UpdateCodexModelQualityPolicy(context.Background(), policy)
	require.NoError(t, err)
	require.Equal(t, policy, saved)
	loaded, err := svc.GetCodexModelQualityPolicy(context.Background())
	require.NoError(t, err)
	require.Equal(t, policy, loaded)
	require.Equal(t, 1, repo.writes)
	require.Len(t, repo.values, len(before)+1)
	for key, value := range before {
		require.Equal(t, value, repo.values[key], key)
	}
}

func TestCodexModelQualityPolicyBudgetBoundaries(t *testing.T) {
	tests := []struct {
		name             string
		minimum, maximum int
		set              func(*CodexModelQualityPolicy, int)
	}{
		{"interval", 300, 86400, func(p *CodexModelQualityPolicy, n int) { p.IntervalSeconds = n }},
		{"timeout", 5, 120, func(p *CodexModelQualityPolicy, n int) { p.TimeoutSeconds = n }},
		{"reserve", 5, 600, func(p *CodexModelQualityPolicy, n int) { p.ReserveSeconds = n }},
		{"ttl percent", 1, 25, func(p *CodexModelQualityPolicy, n int) { p.MaxTTLPercent = n }},
		{"concurrency", 1, 64, func(p *CodexModelQualityPolicy, n int) { p.Concurrency = n }},
		{"account concurrency", 1, 64, func(p *CodexModelQualityPolicy, n int) { p.AccountConcurrency = n }},
		{"retry interval", 60, 3600, func(p *CodexModelQualityPolicy, n int) { p.RetryIntervalSeconds = n }},
		{"low quality threshold", 1, 100, func(p *CodexModelQualityPolicy, n int) { p.LowQualityConsecutiveThreshold = n }},
		{"low quality cooldown", 60, 86400, func(p *CodexModelQualityPolicy, n int) { p.LowQualityCooldownSeconds = n }},
		{"replacement check delay", 60, 86400, func(p *CodexModelQualityPolicy, n int) { p.ReplacementCheckDelaySeconds = n }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repo := &modelQualitySettingsRepo{}
			svc := &SettingService{settingRepo: repo}
			for _, value := range []int{test.minimum, test.maximum} {
				policy := DefaultCodexModelQualityPolicy()
				test.set(&policy, value)
				_, err := svc.UpdateCodexModelQualityPolicy(context.Background(), policy)
				require.NoError(t, err)
				loaded, err := svc.GetCodexModelQualityPolicy(context.Background())
				require.NoError(t, err)
				require.Equal(t, policy, loaded)
			}
			before := maps.Clone(repo.values)
			for _, value := range []int{test.minimum - 1, test.maximum + 1} {
				policy := DefaultCodexModelQualityPolicy()
				test.set(&policy, value)
				_, err := svc.UpdateCodexModelQualityPolicy(context.Background(), policy)
				require.Error(t, err)
				require.Equal(t, before, repo.values)
			}
			require.Equal(t, 2, repo.writes)
		})
	}
}

func TestCodexModelQualityPolicyLegacyConcurrencyDefaultsAccountLimit(t *testing.T) {
	repo := &modelQualitySettingsRepo{values: map[string]string{
		SettingKeyCodexModelQuality: "{\"concurrency\":3}",
	}}
	svc := &SettingService{settingRepo: repo}
	policy, err := svc.GetCodexModelQualityPolicy(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, policy.Concurrency)
	require.Equal(t, 3, policy.AccountConcurrency)
	require.Equal(t, 3, policy.LowQualityConsecutiveThreshold)
	require.Equal(t, 900, policy.LowQualityCooldownSeconds)
	require.Equal(t, 300, policy.ReplacementCheckDelaySeconds)
}

func TestCodexModelQualityPolicyReasoningEffort(t *testing.T) {
	repo := &modelQualitySettingsRepo{}
	svc := &SettingService{settingRepo: repo}
	for _, effort := range []string{"low", "medium", "high"} {
		policy := DefaultCodexModelQualityPolicy()
		policy.ReasoningEffort = effort
		_, err := svc.UpdateCodexModelQualityPolicy(context.Background(), policy)
		require.NoError(t, err)
	}
	before := maps.Clone(repo.values)
	for _, effort := range []string{"", "minimal", "xhigh", "LOW", " high "} {
		policy := DefaultCodexModelQualityPolicy()
		policy.ReasoningEffort = effort
		_, err := svc.UpdateCodexModelQualityPolicy(context.Background(), policy)
		require.Error(t, err)
		require.Equal(t, before, repo.values)
	}
	require.Equal(t, 3, repo.writes)
}

func TestCodexModelQualityPolicyFailedWritePreservesActivePolicy(t *testing.T) {
	repo := &modelQualitySettingsRepo{}
	svc := &SettingService{settingRepo: repo}
	initial := DefaultCodexModelQualityPolicy()
	_, err := svc.UpdateCodexModelQualityPolicy(context.Background(), initial)
	require.NoError(t, err)
	repo.writeErr = errors.New("write unavailable")
	changed := initial
	changed.Enabled, changed.TimeoutSeconds = true, 90
	_, err = svc.UpdateCodexModelQualityPolicy(context.Background(), changed)
	require.ErrorIs(t, err, repo.writeErr)
	loaded, err := svc.GetCodexModelQualityPolicy(context.Background())
	require.NoError(t, err)
	require.Equal(t, initial, loaded)
	require.Equal(t, 1, repo.writes)
}

func TestCodexModelQualityPolicyRejectsCorruptStorage(t *testing.T) {
	for _, raw := range []string{"malformed", "null", "[]", `{"enabled":"true"}`, `{"enabled":true,"timeout_seconds":0}`, `{"enabled":true} {}`} {
		t.Run(raw, func(t *testing.T) {
			repo := &modelQualitySettingsRepo{values: map[string]string{SettingKeyCodexModelQuality: raw}}
			svc := &SettingService{settingRepo: repo}
			_, err := svc.GetCodexModelQualityPolicy(context.Background())
			require.Error(t, err)
			require.Zero(t, repo.writes)
			require.Equal(t, raw, repo.values[SettingKeyCodexModelQuality])
		})
	}
}

func TestCodexModelQualityPolicyUnavailableStorage(t *testing.T) {
	for _, svc := range []*SettingService{nil, {}} {
		policy, err := svc.GetCodexModelQualityPolicy(context.Background())
		require.Error(t, err)
		require.False(t, policy.Enabled)
		_, err = svc.UpdateCodexModelQualityPolicy(context.Background(), DefaultCodexModelQualityPolicy())
		require.Error(t, err)
	}
	repo := &modelQualitySettingsRepo{readErr: errors.New("read unavailable")}
	svc := &SettingService{settingRepo: repo}
	policy, err := svc.GetCodexModelQualityPolicy(context.Background())
	require.ErrorIs(t, err, repo.readErr)
	require.False(t, policy.Enabled)
	require.Zero(t, repo.writes)
	encoded, err := json.Marshal(policy)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "read unavailable")
}
