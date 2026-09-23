package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyPoolRegionChangesReleasePreviousAffinity(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2))
	ctx := context.Background()
	for id, country := range map[int64]string{1: "JP", 2: "PH"} {
		require.NoError(t, a.latencyCache.SetProxyLatency(ctx, id, &service.ProxyLatencyInfo{
			Success: true, CountryCode: country, UpdatedAt: time.Now(),
		}))
	}
	selection := service.ProxyPoolSelection{AccountID: 42, CountryCode: "JP"}
	first, err := a.Select(ctx, selection)
	require.NoError(t, err)
	require.EqualValues(t, 1, first.ID)
	again, err := a.Select(ctx, selection)
	require.NoError(t, err)
	require.Equal(t, first.ID, again.ID)
	selection.CountryCode = "PH"
	second, err := a.Select(ctx, selection)
	require.NoError(t, err)
	require.EqualValues(t, 2, second.ID)
	require.Zero(t, a.rdb.ZCard(ctx, proxyPoolLeaseKey("1")).Val())
	selection.CountryCode = "US"
	missing, err := a.Select(ctx, selection)
	require.NoError(t, err)
	require.Nil(t, missing)
	require.Zero(t, a.rdb.Exists(ctx, proxyPoolAffinityKey("42")).Val())
	require.Zero(t, a.rdb.ZCard(ctx, proxyPoolLeaseKey("2")).Val())
}

func TestProxyPoolRegionRejectsUnknownFailedAndStaleProbe(t *testing.T) {
	for _, reason := range []string{"missing", "country_missing", "failed", "stale", "other_country"} {
		t.Run(reason, func(t *testing.T) {
			candidate := poolCandidate(1)
			candidate.proxy.UpdatedAt = time.Now()
			a, _ := newProxyPoolAllocatorTest(t, 0, candidate)
			ctx := context.Background()
			info := &service.ProxyLatencyInfo{Success: true, CountryCode: "JP", UpdatedAt: candidate.proxy.UpdatedAt}
			switch reason {
			case "country_missing":
				info.CountryCode = ""
			case "failed":
				info.Success = false
			case "stale":
				info.UpdatedAt = info.UpdatedAt.Add(-time.Second)
			case "other_country":
				info.CountryCode = "PH"
			}
			if reason != "missing" {
				require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 1, info))
			}
			selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 42, CountryCode: "JP"})
			require.NoError(t, err)
			require.Nil(t, selected)
		})
	}
}

func TestProxyPoolRegionMatcherRequiresCurrentProbe(t *testing.T) {
	proxy := poolCandidate(1).proxy
	proxy.UpdatedAt = time.Now()
	a, _ := newProxyPoolAllocatorTest(t, 0)
	r := &accountRepository{proxyPool: a}
	ctx := context.Background()
	require.NoError(t, a.latencyCache.SetProxyLatency(ctx, proxy.ID, &service.ProxyLatencyInfo{
		Success: true, CountryCode: "jp", UpdatedAt: proxy.UpdatedAt,
	}))
	matched, err := r.MatchesProxyRegion(ctx, proxy, "JP")
	require.NoError(t, err)
	require.True(t, matched)
	proxy.UpdatedAt = proxy.UpdatedAt.Add(time.Second)
	matched, err = r.MatchesProxyRegion(ctx, proxy, "JP")
	require.NoError(t, err)
	require.False(t, matched)
}

func TestProxyPoolRegionUsesManualCountryOnlyWhenProbeHasNoCountry(t *testing.T) {
	ctx := context.Background()
	proxy := poolCandidate(1).proxy
	proxy.UpdatedAt = time.Now()
	proxy.CountryCode = "JP"
	a, _ := newProxyPoolAllocatorTest(t, 0, proxyPoolCandidate{proxy: proxy})

	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 42, CountryCode: "JP"})
	require.NoError(t, err)
	require.NotNil(t, selected)

	require.NoError(t, a.latencyCache.SetProxyLatency(ctx, proxy.ID, &service.ProxyLatencyInfo{
		Success: true, CountryCode: "PH", UpdatedAt: proxy.UpdatedAt,
	}))
	selected, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 43, CountryCode: "JP"})
	require.NoError(t, err)
	require.Nil(t, selected, "a confirmed probe country overrides manual metadata")

	require.NoError(t, a.latencyCache.SetProxyLatency(ctx, proxy.ID, &service.ProxyLatencyInfo{
		Success: true, CountryCode: "", UpdatedAt: proxy.UpdatedAt,
	}))
	selected, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 44, CountryCode: "JP"})
	require.NoError(t, err)
	require.NotNil(t, selected)
}

func TestProxyPoolRegionIntersectsAllGroupAndSelectedScopes(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	a, _ := newProxyPoolAllocatorTest(t, 0)
	a.client, a.loadCandidates = client, a.readCandidates
	r := newAccountRepositoryWithSQL(client, nil, nil)
	r.proxyPool = a
	ctx := context.Background()
	group, err := client.ProxyGroup.Create().SetName("regional").Save(ctx)
	require.NoError(t, err)
	jp := createPoolTestProxy(t, client, "jp")
	ph := createPoolTestProxy(t, client, "ph")
	_, err = ph.Update().SetGroupID(group.ID).Save(ctx)
	require.NoError(t, err)
	for id, country := range map[int64]string{jp.ID: "JP", ph.ID: "PH"} {
		require.NoError(t, a.latencyCache.SetProxyLatency(ctx, id, &service.ProxyLatencyInfo{
			Success: true, CountryCode: country, UpdatedAt: time.Now(),
		}))
	}
	account := &service.Account{ID: 42, Extra: map[string]any{
		service.ProxyModeExtraKey: service.ProxyModeRandom, "proxy_region_mode": "manual", "proxy_region_country": "JP",
	}}
	require.NoError(t, service.ResolveRandomProxy(ctx, account, r))
	require.Equal(t, jp.ID, *account.ProxyID)
	for _, scope := range []string{service.RandomProxyPoolGroup, service.RandomProxyPoolSelected} {
		account.Extra[service.RandomProxyPoolScopeExtraKey] = scope
		account.Extra[service.RandomProxyGroupIDExtraKey] = group.ID
		account.Extra[service.RandomProxyPoolIDsExtraKey] = []int64{ph.ID}
		require.ErrorIs(t, service.ResolveRandomProxy(ctx, account, r), service.ErrRandomProxyUnavailable)
		require.Nil(t, account.ProxyID, "同国家代理不能突破指定范围")
	}
}
