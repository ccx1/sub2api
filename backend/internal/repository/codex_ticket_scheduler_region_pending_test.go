package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexSchedulerRegionPendingNeverFallsBackToDirect(t *testing.T) {
	for _, tc := range []struct {
		name           string
		allowDirect    bool
		allowFallback  bool
		defaultCountry string
	}{
		{name: "direct_policy", allowDirect: true},
		{name: "direct_policy_with_country_fallback", allowDirect: true, allowFallback: true},
		{name: "direct_policy_with_unavailable_default", allowDirect: true, defaultCountry: "US"},
		{name: "non_direct_policy"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, req := newCodexRegionPendingFixture(t)
			req.AllowDirectOnEmpty = tc.allowDirect
			req.Selection.AllowCountryFallback = tc.allowFallback
			req.Selection.FallbackCountryCode = tc.defaultCountry
			reservation, err := a.ReserveCodexTicket(context.Background(), req)
			requireCodexWait(t, err, "proxy_unhealthy")
			require.Nil(t, reservation, "地区代理探测失败不能授权直连")
		})
	}
}

func TestCodexSchedulerRegionPendingKeepsHealthyFallback(t *testing.T) {
	for _, tc := range []struct {
		name, country  string
		allowFallback  bool
		defaultCountry string
	}{
		{name: "same_region", country: "JP"},
		{name: "default_country", country: "US", defaultCountry: "US"},
		{name: "pool_fallback", country: "US", allowFallback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, req := newCodexRegionPendingFixture(t)
			req.Selection.AllowCountryFallback = tc.allowFallback
			req.Selection.FallbackCountryCode = tc.defaultCountry
			a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
				return []proxyPoolCandidate{codexPoolCandidate(1), codexPoolCandidate(2)}, nil
			}
			require.NoError(t, a.latencyCache.SetProxyLatency(context.Background(), 2, &service.ProxyLatencyInfo{
				Success: true, CountryCode: tc.country, UpdatedAt: time.Now(),
			}))
			reservation := codexStart(t, a, req)
			require.NotNil(t, reservation.Proxy)
			require.EqualValues(t, 2, reservation.Proxy.ID)
			codexFinish(t, a, reservation, "canceled")
		})
	}
}

func TestCodexSchedulerRegionPendingDoesNotDuplicateCandidates(t *testing.T) {
	a, req := newCodexRegionPendingFixture(t)
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		candidate := codexPoolCandidate(1)
		candidate.proxy.CountryCode = "JP"
		return []proxyPoolCandidate{candidate}, nil
	}
	candidates, _, err := a.codexSchedulerCandidates(context.Background(), req)
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "1", candidates[0].ID)
	require.False(t, candidates[0].Healthy)
}

func newCodexRegionPendingFixture(t *testing.T) (*ProxyPoolAllocator, service.CodexTicketReserveRequest) {
	t.Helper()
	a, _, req := newCodexPinTest(t, true)
	req.Manual = false
	req.AllowDirectOnEmpty, req.HarvestUsesBusiness = true, true
	req.Selection.CountryCode = "JP"
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		return []proxyPoolCandidate{codexPoolCandidate(1)}, nil
	}
	require.NoError(t, a.latencyCache.SetProxyLatency(context.Background(), 1, &service.ProxyLatencyInfo{
		Success: false, CountryCode: "JP", UpdatedAt: time.Now(),
	}))
	return a, req
}
