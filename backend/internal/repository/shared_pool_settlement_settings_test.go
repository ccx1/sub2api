//go:build sharedpoolintegration

package repository

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolSettlementSettingsPostgresGlobalRoundTrip(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	initial, err := f.repo.SharedSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 1.0, initial.SettlementMultiplier, "旧配置迁移后应默认按原始费用一倍结算")
	for _, multiplier := range []float64{0, 0.123456, 1.25, 100} {
		settings := sharedSubscriptionSettings(f)
		settings.SettlementMultiplier = multiplier
		require.NoError(t, f.repo.SaveSharedSettings(ctx, settings))
		saved, err := f.repo.SharedSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, settings, saved)
		config, err := service.NewSharedPoolService(f.repo, nil, nil, nil, nil, nil, nil).UserConfig(ctx, f.owner)
		require.NoError(t, err)
		require.Equal(t, multiplier, config["settlement_multiplier"])
	}
}

func TestSharedPoolSettlementSettingsPostgresUserOverrideAndNullInheritance(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	settings := sharedSubscriptionSettings(f)
	require.NoError(t, f.repo.SaveSharedSettings(ctx, settings))
	pool := service.NewSharedPoolService(f.repo, nil, nil, nil, nil, nil, nil)
	platform, proxy := 750, 125
	for _, multiplier := range []*float64{new(0.75), new(0.0), new(100.0), nil} {
		input := service.SharedPoolUserRate{UserID: f.owner, PlatformRateBPS: &platform, ProxyRateBPS: &proxy, SettlementMultiplier: multiplier}
		require.NoError(t, f.repo.SaveSharedUserRate(ctx, input))
		rates, err := f.repo.SharedUserRates(ctx)
		require.NoError(t, err)
		require.Len(t, rates, 1)
		require.Equal(t, input.UserID, rates[0].UserID)
		require.Equal(t, input.PlatformRateBPS, rates[0].PlatformRateBPS)
		require.Equal(t, input.ProxyRateBPS, rates[0].ProxyRateBPS)
		require.Equal(t, multiplier, rates[0].SettlementMultiplier)
		expected := settings.SettlementMultiplier
		if multiplier != nil {
			expected = *multiplier
		}
		config, err := pool.UserConfig(ctx, f.owner)
		require.NoError(t, err)
		require.Equal(t, expected, config["settlement_multiplier"])
		require.Equal(t, platform, config["platform_rate_bps"])
		other, err := pool.UserConfig(ctx, f.other)
		require.NoError(t, err)
		require.Equal(t, settings.SettlementMultiplier, other["settlement_multiplier"])
	}
	settings.SettlementMultiplier = 2.5
	require.NoError(t, f.repo.SaveSharedSettings(ctx, settings))
	config, err := pool.UserConfig(ctx, f.owner)
	require.NoError(t, err)
	require.Equal(t, 2.5, config["settlement_multiplier"], "null 覆盖应持续继承后续全局变化")
}

func TestSharedPoolSettlementSettingsPostgresZeroOverrideSurvivesGlobalChange(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	settings := sharedSubscriptionSettings(f)
	require.NoError(t, f.repo.SaveSharedSettings(ctx, settings))
	require.NoError(t, f.repo.SaveSharedUserRate(ctx, service.SharedPoolUserRate{UserID: f.owner, SettlementMultiplier: new(0.0)}))
	require.NoError(t, f.repo.SaveSharedUserRate(ctx, service.SharedPoolUserRate{UserID: f.other}))
	settings.SettlementMultiplier = 0.5
	require.NoError(t, f.repo.SaveSharedSettings(ctx, settings))
	pool := service.NewSharedPoolService(f.repo, nil, nil, nil, nil, nil, nil)
	owner, err := pool.UserConfig(ctx, f.owner)
	require.NoError(t, err)
	require.Equal(t, 0.0, owner["settlement_multiplier"], "显式零不能视为继承")
	other, err := pool.UserConfig(ctx, f.other)
	require.NoError(t, err)
	require.Equal(t, 0.5, other["settlement_multiplier"])
	rates, err := f.repo.SharedUserRates(ctx)
	require.NoError(t, err)
	require.Len(t, rates, 2)
	for _, rate := range rates {
		if rate.UserID == f.owner {
			require.NotNil(t, rate.SettlementMultiplier)
			require.Zero(t, *rate.SettlementMultiplier)
		} else {
			require.Nil(t, rate.SettlementMultiplier)
		}
	}
}

func TestSharedPoolSettlementSettingsPostgresInvalidGlobalRollsBackAllFields(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	original := sharedSubscriptionSettings(f)
	require.NoError(t, f.repo.SaveSharedSettings(ctx, original))
	for _, value := range []float64{-0.000001, 100.000001, math.NaN(), math.Inf(1), math.Inf(-1)} {
		changed := sharedSubscriptionSettings(f)
		changed.SettlementMultiplier = value
		changed.PlatformRateBPS, changed.ProxyRateBPS, changed.MaxConcurrency = 900, 150, 20
		changed.DefaultGroupIDs[service.PlatformOpenAI] = []int64{f.b}
		changed.SubscriptionGroupIDs[service.PlatformOpenAI]["plus"] = []int64{f.a}
		require.Error(t, f.repo.SaveSharedSettings(ctx, changed))
		saved, err := f.repo.SharedSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, original, saved)
	}
}

func TestSharedPoolSettlementSettingsPostgresLaterWriteFailureRollsBackMultiplier(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	original := sharedSubscriptionSettings(f)
	require.NoError(t, f.repo.SaveSharedSettings(ctx, original))
	changed := sharedSubscriptionSettings(f)
	changed.SettlementMultiplier = 2.5
	changed.MaxConcurrency = 0
	changed.DefaultGroupIDs[service.PlatformOpenAI] = []int64{f.b}
	// 结算倍率 UPDATE 在其它配置 UPDATE 之前，后者违反约束时前者也必须回滚。
	require.Error(t, f.repo.SaveSharedSettings(ctx, changed))
	saved, err := f.repo.SharedSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, original, saved)
}

func TestSharedPoolSettlementSettingsPostgresInvalidUserPreservesOverride(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	require.NoError(t, f.repo.SaveSharedSettings(ctx, sharedSubscriptionSettings(f)))
	require.NoError(t, f.repo.SaveSharedUserRate(ctx, service.SharedPoolUserRate{
		UserID: f.owner, PlatformRateBPS: new(750), ProxyRateBPS: new(125), SettlementMultiplier: new(0.75),
	}))
	original, err := f.repo.SharedUserRates(ctx)
	require.NoError(t, err)
	for _, value := range []float64{-0.000001, 100.000001, math.NaN(), math.Inf(1), math.Inf(-1)} {
		require.Error(t, f.repo.SaveSharedUserRate(ctx, service.SharedPoolUserRate{
			UserID: f.owner, PlatformRateBPS: new(900), ProxyRateBPS: new(150), SettlementMultiplier: &value,
		}))
		saved, err := f.repo.SharedUserRates(ctx)
		require.NoError(t, err)
		require.Equal(t, original, saved)
	}
	require.Error(t, f.repo.SaveSharedUserRate(ctx, service.SharedPoolUserRate{
		UserID: f.owner, PlatformRateBPS: new(10000), ProxyRateBPS: new(1), SettlementMultiplier: new(0.0),
	}))
	saved, err := f.repo.SharedUserRates(ctx)
	require.NoError(t, err)
	require.Equal(t, original, saved, "费率冲突失败不能先更新结算倍率")
}

func TestSharedPoolSettlementTermsPostgresIndependentOverrides(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	require.NoError(t, f.repo.SaveSharedSettings(ctx, sharedSubscriptionSettings(f)))
	require.NoError(t, f.repo.SaveSharedUserRate(ctx, service.SharedPoolUserRate{
		UserID: f.other, SettlementMultiplier: new(3.0), PlatformRateBPS: new(1000), ProxyRateBPS: new(200),
	}))
	global := service.SharedPoolSettlementTerms{Multiplier: 1.25, PlatformRateBPS: 500, ProxyRateBPS: 100}
	terms, err := f.repo.accounts.SharedPoolSettlementTerms(ctx, f.owner)
	require.NoError(t, err)
	require.Equal(t, &global, terms, "无本用户覆盖时回退全局，不能借用其它用户覆盖")
	for _, tc := range []struct {
		name       string
		multiplier *float64
		platform   *int
		proxy      *int
		want       service.SharedPoolSettlementTerms
	}{
		{"multiplier only", new(0.75), nil, nil, service.SharedPoolSettlementTerms{Multiplier: 0.75, PlatformRateBPS: 500, ProxyRateBPS: 100}},
		{"platform only", nil, new(750), nil, service.SharedPoolSettlementTerms{Multiplier: 1.25, PlatformRateBPS: 750, ProxyRateBPS: 100}},
		{"proxy only", nil, nil, new(250), service.SharedPoolSettlementTerms{Multiplier: 1.25, PlatformRateBPS: 500, ProxyRateBPS: 250}},
		{"all fields", new(0.5), new(800), new(175), service.SharedPoolSettlementTerms{Multiplier: 0.5, PlatformRateBPS: 800, ProxyRateBPS: 175}},
		{"explicit zeros", new(0.0), new(0), new(0), service.SharedPoolSettlementTerms{}},
		{"null resets inheritance", nil, nil, nil, global},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, f.repo.SaveSharedUserRate(ctx, service.SharedPoolUserRate{
				UserID: f.owner, SettlementMultiplier: tc.multiplier, PlatformRateBPS: tc.platform, ProxyRateBPS: tc.proxy,
			}))
			terms, err := f.repo.accounts.SharedPoolSettlementTerms(ctx, f.owner)
			require.NoError(t, err)
			require.Equal(t, &tc.want, terms)
		})
	}
}

func TestSharedPoolSettlementTermsPostgresRejectsUnavailableOwner(t *testing.T) {
	for _, state := range []string{"missing", "inactive", "deleted"} {
		t.Run(state, func(t *testing.T) {
			f := sharedAccountPostgresFixture(t)
			ctx := context.Background()
			require.NoError(t, f.repo.SaveSharedSettings(ctx, sharedSubscriptionSettings(f)))
			require.NoError(t, f.repo.SaveSharedUserRate(ctx, service.SharedPoolUserRate{UserID: f.owner, SettlementMultiplier: new(0.75)}))
			ownerID := f.owner
			switch state {
			case "missing":
				ownerID = 999999999
			case "inactive":
				require.NoError(t, f.client.User.UpdateOneID(ownerID).SetStatus("disabled").Exec(ctx))
			case "deleted":
				require.NoError(t, f.client.User.UpdateOneID(ownerID).SetDeletedAt(time.Now()).Exec(ctx))
			}
			terms, err := f.repo.accounts.SharedPoolSettlementTerms(ctx, ownerID)
			require.ErrorIs(t, err, service.ErrSharedPoolBillingInvalid)
			require.Nil(t, terms, "用户不可用时不能回退到全局条款继续结算")
		})
	}
}

func TestSharedPoolSettlementTermsPostgresRejectsInvalidInheritedFeeSum(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	require.NoError(t, f.repo.SaveSharedSettings(ctx, sharedSubscriptionSettings(f)))
	// 单字段合法但与继承的代理费合计超限，模拟绕过管理入口的旧数据。
	_, err := f.db.Exec(`INSERT INTO shared_pool_user_rates(user_id, platform_rate_bps) VALUES ($1, 9901)`, f.owner)
	require.NoError(t, err)
	terms, err := f.repo.accounts.SharedPoolSettlementTerms(ctx, f.owner)
	require.ErrorIs(t, err, service.ErrSharedPoolBillingInvalid)
	require.Nil(t, terms)
}
