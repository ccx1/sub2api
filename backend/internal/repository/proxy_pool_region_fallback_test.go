package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// regionFallbackProxy 构造一条带当前出口探测结果的代理；success=false 表示探测失败（不可用）。
func regionFallbackProxy(t *testing.T, a *ProxyPoolAllocator, id int64, country string, success bool) proxyPoolCandidate {
	t.Helper()
	candidate := poolCandidate(id)
	candidate.proxy.UpdatedAt = time.Now()
	candidate.proxy.CountryCode = country
	require.NoError(t, a.latencyCache.SetProxyLatency(context.Background(), id, &service.ProxyLatencyInfo{
		Success: success, CountryCode: country, UpdatedAt: candidate.proxy.UpdatedAt,
	}))
	return candidate
}

func newRegionFallbackAllocator(t *testing.T, build func(*ProxyPoolAllocator) []proxyPoolCandidate) (*ProxyPoolAllocator, *accountRepository) {
	t.Helper()
	a, _ := newProxyPoolAllocatorTest(t, 0)
	candidates := build(a)
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		return append([]proxyPoolCandidate{}, candidates...), nil
	}
	return a, &accountRepository{proxyPool: a}
}

// billingRegionAccount 模拟「按账单地区」+「默认代理地区」+ 空池禁用策略的随机代理账号。
func billingRegionAccount(id int64, priceCountry, defaultCountry, regionFallback string) *service.Account {
	extra := map[string]any{
		service.ProxyModeExtraKey:                  service.ProxyModeRandom,
		service.RandomProxyEmptyPoolPolicyExtraKey: service.RandomProxyEmptyPoolPolicyDisable,
		service.ProxyRegionModeExtraKey:            "billing",
		service.ProxyRegionFallbackCountryExtraKey: defaultCountry,
	}
	if regionFallback != "" {
		extra[service.RandomProxyRegionFallbackExtraKey] = regionFallback
	}
	return &service.Account{ID: id, Credentials: map[string]any{"price_country": priceCountry}, Extra: extra}
}

func TestProxyPoolRegionFallbackTiers(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name           string
		priceCountry   string
		regionFallback string
		build          func(*ProxyPoolAllocator) []proxyPoolCandidate
		wantIDs        []int64
		wantFallback   bool
	}{
		{
			name: "billing region has active proxy", priceCountry: "JP",
			build: func(a *ProxyPoolAllocator) []proxyPoolCandidate {
				return []proxyPoolCandidate{regionFallbackProxy(t, a, 1, "JP", true), regionFallbackProxy(t, a, 2, "GB", true), regionFallbackProxy(t, a, 3, "US", true)}
			},
			wantIDs: []int64{1},
		},
		{
			name: "billing region without proxy uses default region", priceCountry: "JP",
			build: func(a *ProxyPoolAllocator) []proxyPoolCandidate {
				return []proxyPoolCandidate{regionFallbackProxy(t, a, 2, "GB", true), regionFallbackProxy(t, a, 3, "US", true)}
			},
			wantIDs: []int64{2}, wantFallback: true,
		},
		{
			name: "billing and default region without proxy use whole pool", priceCountry: "JP",
			build: func(a *ProxyPoolAllocator) []proxyPoolCandidate {
				return []proxyPoolCandidate{regionFallbackProxy(t, a, 3, "US", true), regionFallbackProxy(t, a, 4, "BR", true)}
			},
			wantIDs: []int64{3, 4}, wantFallback: true,
		},
		{
			name: "configured but inactive region proxies still fall back", priceCountry: "JP",
			build: func(a *ProxyPoolAllocator) []proxyPoolCandidate {
				failedProbe := regionFallbackProxy(t, a, 1, "JP", false)
				disabled := regionFallbackProxy(t, a, 5, "JP", true)
				disabled.proxy.Status = service.StatusDisabled
				failedDefault := regionFallbackProxy(t, a, 2, "GB", false)
				return []proxyPoolCandidate{failedProbe, disabled, failedDefault, regionFallbackProxy(t, a, 3, "US", true)}
			},
			wantIDs: []int64{3}, wantFallback: true,
		},
		{
			name: "billing region aliases are normalized", priceCountry: " usa ",
			build: func(a *ProxyPoolAllocator) []proxyPoolCandidate {
				manual := poolCandidate(6)
				manual.proxy.CountryCode = "us"
				return []proxyPoolCandidate{manual, regionFallbackProxy(t, a, 2, "GB", true)}
			},
			wantIDs: []int64{6},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, r := newRegionFallbackAllocator(t, tc.build)
			account := billingRegionAccount(42, tc.priceCountry, "GB", tc.regionFallback)
			require.NoError(t, service.ResolveRandomProxy(ctx, account, r))
			require.NotNil(t, account.ProxyID)
			require.Contains(t, tc.wantIDs, *account.ProxyID)
			require.Equal(t, tc.wantFallback, account.Proxy.RegionFallback)
		})
	}
}

func TestProxyPoolRegionFallbackPolicyWhenNothingUsable(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name           string
		regionFallback string
		build          func(*ProxyPoolAllocator) []proxyPoolCandidate
	}{
		{
			// 默认地区也无代理且关闭“回退到整个池”：即使池里有其它地区的可用代理也不能使用。
			name: "fallback switch disabled", regionFallback: service.RandomProxyRegionFallbackNone,
			build: func(a *ProxyPoolAllocator) []proxyPoolCandidate {
				return []proxyPoolCandidate{regionFallbackProxy(t, a, 3, "US", true), regionFallbackProxy(t, a, 4, "BR", true)}
			},
		},
		{
			name: "whole pool has no active proxy", regionFallback: service.RandomProxyRegionFallbackPool,
			build: func(a *ProxyPoolAllocator) []proxyPoolCandidate {
				disabled := regionFallbackProxy(t, a, 3, "US", true)
				disabled.proxy.Status = service.StatusDisabled
				return []proxyPoolCandidate{regionFallbackProxy(t, a, 1, "JP", false), regionFallbackProxy(t, a, 2, "GB", false), disabled}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, r := newRegionFallbackAllocator(t, tc.build)
			account := billingRegionAccount(42, "JP", "GB", tc.regionFallback)
			err := service.ResolveRandomProxy(ctx, account, r)
			require.ErrorIs(t, err, service.ErrRandomProxyUnavailable)
			require.Equal(t, service.RandomProxyEmptyPoolPolicyDisable, service.RandomProxyUnavailablePolicy(err))
			require.EqualError(t, err, "random proxy pool has no active proxies; policy=disable")
			require.Nil(t, account.ProxyID)
		})
	}
}

func TestProxyPoolRegionDefaultCountryIgnoresPoolFallbackSwitch(t *testing.T) {
	_, r := newRegionFallbackAllocator(t, func(a *ProxyPoolAllocator) []proxyPoolCandidate {
		return []proxyPoolCandidate{regionFallbackProxy(t, a, 2, "GB", true), regionFallbackProxy(t, a, 3, "US", true)}
	})
	account := billingRegionAccount(42, "JP", "GB", service.RandomProxyRegionFallbackNone)
	require.NoError(t, service.ResolveRandomProxy(context.Background(), account, r))
	require.EqualValues(t, 2, *account.ProxyID, "默认代理地区是用户显式配置的地区，不受“回退到整个池”开关限制")
	require.True(t, account.Proxy.RegionFallback)
}
