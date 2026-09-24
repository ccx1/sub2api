//go:build sharedpoolintegration

package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func sharedSubscriptionSettings(f sharedAccountPG) *service.SharedPoolSettings {
	return &service.SharedPoolSettings{
		PlatformRateBPS: 500, ProxyRateBPS: 100, MaxConcurrency: 10,
		DefaultGroupIDs:      service.SharedPoolDefaultGroupIDs{service.PlatformOpenAI: {f.a}},
		SubscriptionGroupIDs: service.SharedPoolSubscriptionGroupIDs{service.PlatformOpenAI: {"plus": {f.b}}},
		SettlementMultiplier: 1.25,
	}
}

func TestSharedPoolSettingsPostgresSubscriptionRoundTrip(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	s := sharedSubscriptionSettings(f)
	claude, err := f.client.Group.Create().SetName("Claude Pro").SetPlatform(service.PlatformAnthropic).SetIsSharedPool(true).Save(ctx)
	require.NoError(t, err)
	s.DefaultGroupIDs[service.PlatformAnthropic] = []int64{claude.ID}
	s.SubscriptionGroupIDs[service.PlatformAnthropic] = map[string][]int64{"pro": {claude.ID}}
	require.NoError(t, f.repo.SaveSharedSettings(ctx, s))
	saved, err := f.repo.SharedSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, s, saved)
}

func TestSharedPoolSettingsPostgresLegacyPayloadPreservesAndEmptyClears(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	s := sharedSubscriptionSettings(f)
	require.NoError(t, f.repo.SaveSharedSettings(ctx, s))
	legacy := *s
	legacy.SubscriptionGroupIDs = nil
	legacy.PlatformRateBPS = 700
	require.NoError(t, f.repo.SaveSharedSettings(ctx, &legacy))
	require.Equal(t, s.SubscriptionGroupIDs, legacy.SubscriptionGroupIDs, "保存响应必须包含实际保留的档位规则")
	saved, err := f.repo.SharedSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, s.SubscriptionGroupIDs, saved.SubscriptionGroupIDs)
	require.Equal(t, 700, saved.PlatformRateBPS)
	legacy.SubscriptionGroupIDs = service.SharedPoolSubscriptionGroupIDs{}
	require.NoError(t, f.repo.SaveSharedSettings(ctx, &legacy))
	saved, err = f.repo.SharedSettings(ctx)
	require.NoError(t, err)
	require.NotNil(t, saved.SubscriptionGroupIDs)
	require.Empty(t, saved.SubscriptionGroupIDs)
}

func TestSharedPoolSettingsPostgresInvalidSubscriptionRollsBackAllFields(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	original := sharedSubscriptionSettings(f)
	require.NoError(t, f.repo.SaveSharedSettings(ctx, original))
	tests := []struct {
		name   string
		change func(*dbent.GroupCreate) *dbent.GroupCreate
	}{
		{"cross platform", func(g *dbent.GroupCreate) *dbent.GroupCreate { return g.SetPlatform(service.PlatformAnthropic) }},
		{"inactive", func(g *dbent.GroupCreate) *dbent.GroupCreate { return g.SetStatus("inactive") }},
		{"zero charge multiplier", func(g *dbent.GroupCreate) *dbent.GroupCreate { return g.SetRateMultiplier(0) }},
		{"negative charge multiplier", func(g *dbent.GroupCreate) *dbent.GroupCreate { return g.SetRateMultiplier(-1) }},
		{"subscription group", func(g *dbent.GroupCreate) *dbent.GroupCreate { return g.SetSubscriptionType("subscription") }},
		{"deleted", func(g *dbent.GroupCreate) *dbent.GroupCreate { return g.SetDeletedAt(time.Now()) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			group, err := tc.change(f.client.Group.Create().SetName(tc.name).SetPlatform(service.PlatformOpenAI).SetIsSharedPool(true)).Save(ctx)
			require.NoError(t, err)
			changed := sharedSubscriptionSettings(f)
			changed.PlatformRateBPS, changed.MaxConcurrency = 900, 20
			changed.SettlementMultiplier = 2.5
			changed.DefaultGroupIDs[service.PlatformOpenAI] = []int64{f.b}
			changed.SubscriptionGroupIDs[service.PlatformOpenAI]["plus"] = []int64{group.ID}
			require.Error(t, f.repo.SaveSharedSettings(ctx, changed))
			saved, err := f.repo.SharedSettings(ctx)
			require.NoError(t, err)
			require.Equal(t, original, saved)
		})
	}
	missing := sharedSubscriptionSettings(f)
	missing.SubscriptionGroupIDs[service.PlatformOpenAI]["plus"] = []int64{999999999}
	require.Error(t, f.repo.SaveSharedSettings(ctx, missing))
	saved, err := f.repo.SharedSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, original, saved)
}

func TestSharedPoolSettingsPostgresAllowsExclusiveSubscriptionTarget(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	exclusive, err := f.client.Group.Create().SetName("Exclusive tier target").
		SetPlatform(service.PlatformOpenAI).SetIsExclusive(true).SetRateMultiplier(1.5).Save(ctx)
	require.NoError(t, err)
	defaultOnly := sharedSubscriptionSettings(f)
	defaultOnly.DefaultGroupIDs[service.PlatformOpenAI] = []int64{exclusive.ID}
	require.Error(t, f.repo.SaveSharedSettings(ctx, defaultOnly), "exclusive groups remain invalid as defaults")
	settings := sharedSubscriptionSettings(f)
	settings.SubscriptionGroupIDs[service.PlatformOpenAI]["plus"] = []int64{exclusive.ID}
	require.NoError(t, f.repo.SaveSharedSettings(ctx, settings))
	saved, err := f.repo.SharedSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, settings, saved)
}

func TestSharedPoolSettingsPostgresSubscriptionsRequireDefault(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	original := sharedSubscriptionSettings(f)
	require.NoError(t, f.repo.SaveSharedSettings(ctx, original))
	for _, legacy := range []bool{false, true} {
		changed := sharedSubscriptionSettings(f)
		changed.DefaultGroupIDs = service.SharedPoolDefaultGroupIDs{}
		if legacy {
			changed.SubscriptionGroupIDs = nil
		}
		require.Error(t, f.repo.SaveSharedSettings(ctx, changed))
		saved, err := f.repo.SharedSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, original, saved)
	}
}

func TestSharedPoolSettingsPostgresInvalidRatesRollBackSubscriptionChange(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	original := sharedSubscriptionSettings(f)
	require.NoError(t, f.repo.SaveSharedSettings(ctx, original))
	high := 9900
	require.NoError(t, f.repo.SaveSharedUserRate(ctx, service.SharedPoolUserRate{UserID: f.owner, PlatformRateBPS: &high}))
	changed := sharedSubscriptionSettings(f)
	changed.ProxyRateBPS = 101
	changed.SettlementMultiplier = 2.5
	changed.SubscriptionGroupIDs[service.PlatformOpenAI]["plus"] = []int64{f.a}
	require.Error(t, f.repo.SaveSharedSettings(ctx, changed))
	saved, err := f.repo.SharedSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, original, saved)
}

func TestSharedPoolSettingsPostgresMigrationPreservesLegacySettings(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	_, err := f.db.Exec(`ALTER TABLE shared_pool_settings DROP COLUMN subscription_group_ids;
        UPDATE shared_pool_settings SET platform_rate_bps=500,proxy_rate_bps=150,max_concurrency=7`)
	require.NoError(t, err)
	_, err = f.db.Exec(`UPDATE shared_pool_settings SET default_group_ids=jsonb_build_object('openai',$1::bigint)`, f.a)
	require.NoError(t, err)
	body, err := os.ReadFile(filepath.Join("..", "..", "migrations", "252_shared_pool_subscription_groups.sql"))
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = f.db.Exec(string(body))
		require.NoError(t, err, "migration must be idempotent")
	}
	saved, err := f.repo.SharedSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 500, saved.PlatformRateBPS)
	require.Equal(t, 150, saved.ProxyRateBPS)
	require.Equal(t, 7, saved.MaxConcurrency)
	require.Equal(t, service.SharedPoolDefaultGroupIDs{service.PlatformOpenAI: {f.a}}, saved.DefaultGroupIDs)
	require.NotNil(t, saved.SubscriptionGroupIDs)
	require.Empty(t, saved.SubscriptionGroupIDs)
	require.Equal(t, 1.0, saved.SettlementMultiplier)
	_, err = f.db.Exec(`UPDATE shared_pool_settings SET subscription_group_ids=NULL`)
	require.Error(t, err, "the new column must remain non-null")
}

func TestSharedPoolSettingsPostgresOrdinaryStandardGroupsAllowed(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformAnthropic} {
		t.Run(platform, func(t *testing.T) {
			group, err := f.client.Group.Create().SetName("Ordinary " + platform).SetPlatform(platform).
				SetIsSharedPool(false).SetSubscriptionType("standard").SetRateMultiplier(1.5).Save(ctx)
			require.NoError(t, err)
			s := sharedSubscriptionSettings(f)
			s.DefaultGroupIDs[platform] = []int64{group.ID}
			tier := "plus"
			if platform == service.PlatformAnthropic {
				tier = "pro"
			}
			s.SubscriptionGroupIDs[platform] = map[string][]int64{tier: {group.ID}}
			require.NoError(t, f.repo.SaveSharedSettings(ctx, s))
			saved, err := f.repo.SharedSettings(ctx)
			require.NoError(t, err)
			require.Equal(t, s, saved)
			stored, err := f.client.Group.Get(ctx, group.ID)
			require.NoError(t, err)
			require.False(t, stored.IsSharedPool, "允许分配不应隐式公开普通分组")
			require.Equal(t, 1.5, stored.RateMultiplier)
		})
	}
}
