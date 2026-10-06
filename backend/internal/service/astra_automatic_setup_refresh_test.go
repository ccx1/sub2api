package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type astraSetupValidationUpstream struct {
	*astraSetupUpstream
	calls []int64
	fail  int64
	cache []bool
}

func (p *astraSetupValidationUpstream) VerifyAstraGatewayTarget(ctx context.Context, _ *http.Request, _ string, id int64, _ int) error {
	p.calls = append(p.calls, id)
	p.cache = append(p.cache, AstraTargetValidationCacheAllowed(ctx))
	if id == p.fail {
		return errors.New("target_probe_degraded")
	}
	expiry := time.Now().Add(time.Minute)
	p.snapshot.Targets = append(p.snapshot.Targets, AstraRouteStatus{AccountID: id, State: "ready", Reason: "target_probe_passed", ExpiresAt: &expiry})
	return nil
}

func TestAstraAutomaticSetupSkipsHealthyAndContinuesAfterFailure(t *testing.T) {
	settings := config.AstraRoutingSettings{Revision: "current", CookiePool: config.CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{299}, TargetAccountIDs: []int64{300, 301, 302, 303}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	expiry, nearExpiry := time.Now().Add(time.Minute), time.Now().Add(20*time.Second)
	provider := &astraSetupValidationUpstream{fail: 301, astraSetupUpstream: &astraSetupUpstream{
		prepare: func(context.Context) error { return nil },
		snapshot: AstraGatewayRuntime{Revision: "current", Targets: []AstraRouteStatus{
			{AccountID: 300, State: "ready", Reason: "target_probe_passed", ExpiresAt: &expiry},
			{AccountID: 303, State: "ready", Reason: "target_probe_passed", ExpiresAt: &nearExpiry},
		}},
	}}
	repo := &astraSchedulingRepo{accounts: map[int64]*Account{}}
	for _, id := range settings.CookiePool.TargetAccountIDs {
		a := stateProbeAccount()
		a.ID, a.Status = id, StatusActive
		repo.accounts[id] = a
	}
	svc := &AccountTestService{cfg: cfg, httpUpstream: provider, accountRepo: repo, openaiGatewayService: &OpenAIGatewayService{}, astraSetupStatus: AstraSetupStatus{Revision: settings.Revision}}
	ctx, cancel := context.WithCancel(t.Context())
	svc.runAstraAutomaticSetup(ctx, cancel, settings)
	require.Equal(t, []int64{301, 302}, provider.calls, "one failed target must not block later targets or re-probe a still-valid cookie")
	require.Equal(t, []bool{true, true}, provider.cache)
	require.Equal(t, "failed", svc.astraSetupStatus.State)
	require.Equal(t, int64(301), svc.astraSetupStatus.AccountID)
	require.Equal(t, "target_probe_degraded", svc.astraSetupStatus.Reason)
	_, ready := astraTargetsReady(t.Context(), provider, []int64{300, 302, 303})
	require.True(t, ready)
}

func TestAstraAutomaticSetupRejectsStaleGroupMembership(t *testing.T) {
	settings := config.AstraRoutingSettings{Revision: "current", CookiePool: config.CodexGatewayPinConfig{Enabled: true, TargetAccountIDs: []int64{300}}}
	current := settings
	current.CookiePool.TargetAccountIDs = []int64{301}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return current })
	svc := &AccountTestService{cfg: cfg}
	svc.StartAstraAutomaticSetup(settings)
	require.Empty(t, svc.astraSetupStatus.State, "unchanged persisted revision cannot authorize removed group members")
	current.SelectionError = "target_group_unavailable"
	svc.StartAstraAutomaticSetup(current)
	require.Equal(t, "failed", svc.astraSetupStatus.State)
	require.Equal(t, "target_group_unavailable", svc.astraSetupStatus.Reason)
}

func TestAstraAutomaticSetupStopsWhenMembersChangeAfterPreparation(t *testing.T) {
	settings := config.AstraRoutingSettings{Revision: "current", CookiePool: config.CodexGatewayPinConfig{Enabled: true, TargetAccountIDs: []int64{300}}}
	current := settings
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return current })
	provider := &astraSetupValidationUpstream{astraSetupUpstream: &astraSetupUpstream{prepare: func(context.Context) error {
		current.CookiePool.TargetAccountIDs = []int64{301}
		return nil
	}}}
	svc := &AccountTestService{cfg: cfg, httpUpstream: provider, astraSetupStatus: AstraSetupStatus{Revision: settings.Revision}}
	ctx, cancel := context.WithCancel(t.Context())
	svc.runAstraAutomaticSetup(ctx, cancel, settings)
	require.Empty(t, provider.calls)
	require.Equal(t, "failed", svc.astraSetupStatus.State, "must release scheduler busy state after membership changes")
	require.Equal(t, "configuration_changed", svc.astraSetupStatus.Reason)
}

func TestAstraAutomaticSetupRechecksTargetAfterSourceRefresh(t *testing.T) {
	settings := config.AstraRoutingSettings{Revision: "current", CookiePool: config.CodexGatewayPinConfig{Enabled: true, TargetAccountIDs: []int64{300}}}
	cfg := &config.Config{}
	cfg.SetAstraRoutingLoader(func(context.Context) config.AstraRoutingSettings { return settings })
	expiry := time.Now().Add(20 * time.Second)
	provider := &astraSetupValidationUpstream{astraSetupUpstream: &astraSetupUpstream{snapshot: AstraGatewayRuntime{Revision: "current", Targets: []AstraRouteStatus{
		{AccountID: 300, State: "ready", Reason: "target_probe_passed", ExpiresAt: &expiry},
	}}}}
	provider.prepare = func(context.Context) error {
		provider.snapshot.Targets = nil // 新来源 Cookie 使旧目标验证失效。
		return nil
	}
	account := stateProbeAccount()
	account.ID, account.Status = 300, StatusActive
	svc := &AccountTestService{cfg: cfg, httpUpstream: provider, accountRepo: &stateProbeAccountRepo{account: account}, openaiGatewayService: &OpenAIGatewayService{}, astraSetupStatus: AstraSetupStatus{Revision: settings.Revision}}
	ctx, cancel := context.WithCancel(t.Context())
	svc.runAstraAutomaticSetup(ctx, cancel, settings)
	require.Equal(t, []int64{300}, provider.calls)
	require.Equal(t, "ready", svc.astraSetupStatus.State)
}
