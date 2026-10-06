package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type astraGroupSettingsRepo struct {
	*astraSettingsRepo
	sourceIDs, targetIDs []int64
	resolveErr, readErr  error
	seen                 config.AstraRoutingSettings
}

func (r *astraGroupSettingsRepo) ResolveAstraRoutingAccounts(_ context.Context, value config.AstraRoutingSettings) (config.AstraRoutingSettings, error) {
	r.seen = value
	if value.CookiePool.SourceSelection == "groups" {
		value.CookiePool.SourceAccountIDs = append([]int64(nil), r.sourceIDs...)
	}
	if value.CookiePool.TargetSelection == "groups" {
		value.CookiePool.TargetAccountIDs = append([]int64(nil), r.targetIDs...)
	}
	if r.resolveErr != nil {
		return value, r.resolveErr
	}
	return config.ResolveAstraDependencies(value)
}

func (r *astraGroupSettingsRepo) GetValue(ctx context.Context, key string) (string, error) {
	if r.readErr != nil {
		return "", r.readErr
	}
	return r.astraSettingsRepo.GetValue(ctx, key)
}

func astraGroupSettingsFixture() (*SettingService, *astraGroupSettingsRepo, config.AstraRoutingSettings) {
	repo := &astraGroupSettingsRepo{astraSettingsRepo: &astraSettingsRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{}}}, sourceIDs: []int64{10}, targetIDs: []int64{20}}
	svc := NewSettingService(repo, &config.Config{})
	value := config.AstraRoutingSettings{CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceSelection: "groups", TargetSelection: "groups", SourceGroupIDs: []int64{1}, TargetGroupIDs: []int64{2}}}
	return svc, repo, value
}

func TestAstraGroupSettingsPersistSelectorsAndRefreshMembership(t *testing.T) {
	svc, repo, value := astraGroupSettingsFixture()
	value.CookiePool.SourceAccountIDs = []int64{999}
	value.CookiePool.TargetAccountIDs = []int64{998}
	saved, err := svc.SetAstraRouting(t.Context(), value)
	require.NoError(t, err)
	require.Empty(t, repo.seen.CookiePool.SourceAccountIDs)
	require.Empty(t, repo.seen.CookiePool.TargetAccountIDs)
	require.Equal(t, []int64{20}, saved.CookiePool.TargetAccountIDs)
	var stored config.AstraRoutingSettings
	require.NoError(t, json.Unmarshal([]byte(repo.values[astraRoutingSettingKey]), &stored))
	require.Equal(t, []int64{2}, stored.CookiePool.TargetGroupIDs)
	require.Empty(t, stored.CookiePool.TargetAccountIDs)
	repo.targetIDs = []int64{21, 22}
	svc.astraRoutingExpires = time.Time{}
	runtime := svc.cfg.AstraRouting(t.Context())
	require.Equal(t, []int64{21, 22}, runtime.CookiePool.TargetAccountIDs)
	require.Equal(t, saved.Revision, runtime.Revision, "membership refresh does not rewrite persisted policy")
}

func TestAstraGroupSettingsInvalidMembershipRemainsEditable(t *testing.T) {
	svc, repo, value := astraGroupSettingsFixture()
	_, err := svc.SetAstraRouting(t.Context(), value)
	require.NoError(t, err)
	repo.targetIDs = nil
	got, err := svc.GetAstraRouting(t.Context())
	require.NoError(t, err)
	require.Equal(t, "astra_target_required", got.SelectionError)
	require.Equal(t, []int64{2}, got.CookiePool.TargetGroupIDs)
	require.Empty(t, got.CookiePool.TargetAccountIDs)
	before := repo.values[astraRoutingSettingKey]
	_, err = svc.SetAstraRouting(t.Context(), value)
	require.ErrorContains(t, err, "astra_target_required")
	require.Equal(t, before, repo.values[astraRoutingSettingKey])
	value.CookiePool.Enabled = false
	_, err = svc.SetAstraRouting(t.Context(), value)
	require.NoError(t, err, "invalid members must not prevent disabling the policy")
}

func TestAstraGroupSettingsReadFailureDoesNotReuseOldMembers(t *testing.T) {
	svc, repo, value := astraGroupSettingsFixture()
	_, err := svc.SetAstraRouting(t.Context(), value)
	require.NoError(t, err)
	repo.readErr = errors.New("database unavailable")
	svc.astraRoutingExpires = time.Time{}
	runtime := svc.cfg.AstraRouting(t.Context())
	require.Equal(t, "astra_group_resolution_unavailable", runtime.SelectionError)
	require.Empty(t, runtime.CookiePool.SourceAccountIDs)
	require.Empty(t, runtime.CookiePool.TargetAccountIDs)
	repo.readErr = nil
	svc.astraRoutingExpires = time.Time{}
	require.Equal(t, []int64{20}, svc.cfg.AstraRouting(t.Context()).CookiePool.TargetAccountIDs)
}

func TestAstraGroupSettingsResolveDonorBeforeWSTargetExpansion(t *testing.T) {
	svc, repo, value := astraGroupSettingsFixture()
	svc.cfg.Gateway.OpenAIWS = config.GatewayOpenAIWSConfig{Enabled: true, OAuthEnabled: true, ResponsesWebsocketsV2: true}
	value.CookiePool.TargetSelection = "accounts"
	value.CookiePool.TargetGroupIDs = nil
	value.WSSession = config.CodexWSAnchorConfig{Enabled: true, AccountIDs: []int64{10, 30}}
	saved, err := svc.SetAstraRouting(t.Context(), value)
	require.NoError(t, err)
	require.Empty(t, repo.seen.CookiePool.TargetAccountIDs, "group donor must be resolved before WS accounts expand targets")
	require.Equal(t, []int64{30}, saved.CookiePool.TargetAccountIDs)
	require.Equal(t, []int64{10}, saved.CookiePool.SourceAccountIDs)
}

func TestAstraGroupRuntimeShowsSelectionFailureInsteadOfOldReadiness(t *testing.T) {
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings {
		return config.AstraRoutingSettings{Revision: "policy", SelectionError: "astra_group_accounts_empty"}
	})
	svc := &AccountTestService{cfg: cfg, astraSetupStatus: AstraSetupStatus{Revision: "policy", State: "ready", Phase: "complete"}}
	status := svc.AstraGatewayStatus(t.Context())
	require.Equal(t, "failed", status.Setup.State)
	require.Equal(t, "astra_group_accounts_empty", status.Setup.Reason)
	require.Empty(t, status.Setup.Phase)
}

func TestAstraGroupSettingsRefreshExcludesNewSourceMembers(t *testing.T) {
	svc, repo, value := astraGroupSettingsFixture()
	repo.targetIDs = []int64{20, 30}
	_, err := svc.SetAstraRouting(t.Context(), value)
	require.NoError(t, err)
	repo.sourceIDs = []int64{10, 20}
	svc.astraRoutingExpires = time.Time{}
	runtime := svc.cfg.AstraRouting(t.Context())
	require.Equal(t, []int64{10, 20}, runtime.CookiePool.SourceAccountIDs)
	require.Equal(t, []int64{30}, runtime.CookiePool.TargetAccountIDs)
	require.Empty(t, runtime.SelectionError)
	repo.sourceIDs = []int64{10}
	svc.astraRoutingExpires = time.Time{}
	require.Equal(t, []int64{20, 30}, svc.cfg.AstraRouting(t.Context()).CookiePool.TargetAccountIDs, "group targets follow live membership")
}
