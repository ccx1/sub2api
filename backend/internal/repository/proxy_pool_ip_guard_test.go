package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func ipGuardCandidate(id int64, host string) proxyPoolCandidate {
	return proxyPoolCandidate{proxy: &service.Proxy{ID: id, Status: service.StatusActive,
		Protocol: "http", Host: host, Port: 8080}}
}

func seedProxyIPGuard(t *testing.T, a *ProxyPoolAllocator, ip string, disabled bool, cooldown time.Duration) string {
	t.Helper()
	ctx := context.Background()
	now, err := a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	payload, err := json.Marshal(map[string]any{"disabled": disabled, "until_at": now.Add(cooldown).UnixMilli()})
	require.NoError(t, err)
	key := proxyIPGuardKey(ip)
	require.NoError(t, a.rdb.Set(ctx, key, payload, 0).Err())
	return key
}

func TestProxyPoolIPGuardBlocksSharedEgressAndPreservesOtherIPs(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "cooling", true: "disabled"}[disabled], func(t *testing.T) {
			ctx := context.Background()
			first, second := ipGuardCandidate(1, "first.example"), ipGuardCandidate(2, "second.example")
			other := ipGuardCandidate(3, "203.0.113.11")
			a, _ := newProxyPoolAllocatorTest(t, 0, first, second, other)
			for _, proxy := range []*service.Proxy{first.proxy, second.proxy} {
				require.NoError(t, a.latencyCache.SetProxyLatency(ctx, proxy.ID, &service.ProxyLatencyInfo{
					Success: true, IPAddress: "203.0.113.10", UpdatedAt: time.Now(), ProxyIdentity: service.ProxyProbeIdentity(proxy)}))
			}
			seedProxyIPGuard(t, a, "203.0.113.10", disabled, time.Hour)
			for accountID := int64(1); accountID <= 5; accountID++ {
				selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: accountID})
				require.NoError(t, err)
				require.NotNil(t, selected)
				require.Equal(t, other.proxy.ID, selected.ID)
			}
		})
	}
}

func TestProxyPoolIPGuardCannotBeBypassedByAllPoolRecovery(t *testing.T) {
	ctx := context.Background()
	first, second := ipGuardCandidate(1, "203.0.113.10"), ipGuardCandidate(2, "203.0.113.10")
	a, server := newProxyPoolAllocatorTest(t, 0, first, second)
	key := seedProxyIPGuard(t, a, first.proxy.Host, false, time.Minute)
	for _, proxy := range []*service.Proxy{first.proxy, second.proxy} {
		require.NoError(t, a.ReportTransportFailure(ctx, 0, proxy))
	}
	before, err := server.Get(key)
	require.NoError(t, err)
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.Nil(t, selected)
	require.EqualValues(t, 2, a.rdb.ZCard(ctx, proxyTransportCooldownKey).Val())
	selected, err = a.Select(service.WithCodexTicketProxyResolution(ctx), service.ProxyPoolSelection{AccountID: 8})
	require.Error(t, err, "temporary IP blocking must not become an empty-pool direct connection")
	require.Nil(t, selected)
	after, err := server.Get(key)
	require.NoError(t, err)
	require.Equal(t, before, after)
	codexAdvance(t, a, server, time.Minute)
	selected, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.NotNil(t, selected, "normal transport recovery may resume only after IP cooldown expires")
}

func TestProxyIPGuardFixedProxyUsesMatchingCachedEgress(t *testing.T) {
	ctx := context.Background()
	proxy := ipGuardCandidate(1, "fixed.example").proxy
	a, _ := newProxyPoolAllocatorTest(t, 0, proxyPoolCandidate{proxy: proxy})
	info := &service.ProxyLatencyInfo{Success: true, IPAddress: "203.0.113.10", UpdatedAt: time.Now(), ProxyIdentity: service.ProxyProbeIdentity(proxy)}
	require.NoError(t, a.latencyCache.SetProxyLatency(ctx, proxy.ID, info))
	seedProxyIPGuard(t, a, info.IPAddress, true, 0)
	cooling, err := a.TransportCoolingDown(ctx, proxy)
	require.NoError(t, err)
	require.True(t, cooling)
	changed := *proxy
	changed.Host = "203.0.113.11"
	cooling, err = a.TransportCoolingDown(ctx, &changed)
	require.NoError(t, err)
	require.False(t, cooling, "a stale probe IP must not block a changed proxy endpoint")
}

func TestProxyIPGuardFixedFallbackDoesNotReuseBlockedIP(t *testing.T) {
	ctx := context.Background()
	first, second := ipGuardCandidate(1, "203.0.113.10"), ipGuardCandidate(2, "203.0.113.10")
	other := ipGuardCandidate(3, "203.0.113.11")
	a, _ := newProxyPoolAllocatorTest(t, 0, first, second, other)
	repo, mock := newCodexTicketCASRepo(t)
	repo.proxyPool = a
	seedProxyIPGuard(t, a, first.proxy.Host, true, 0)
	expectTransportProxy(mock, first.proxy)
	mock.ExpectQuery(`SELECT .* FROM "proxies" WHERE .*shared_pool_proxies`).WithArgs(first.proxy.ID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(first.proxy.ID))
	selected, err := repo.ResolveFixedProxyFailover(ctx, &service.Account{ID: 7, ProxyID: &first.proxy.ID, Proxy: first.proxy})
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, other.proxy.ID, selected.ID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyIPGuardFailsClosedForRedisAndCorruptState(t *testing.T) {
	for _, state := range []string{"redis_unavailable", "{", `{"disabled":"false"}`, `{"until_at":"bad"}`} {
		t.Run(state, func(t *testing.T) {
			ctx := context.Background()
			candidate := ipGuardCandidate(1, "203.0.113.10")
			a, _ := newProxyPoolAllocatorTest(t, 0, candidate)
			if state == "redis_unavailable" {
				require.NoError(t, a.rdb.Close())
			} else {
				require.NoError(t, a.rdb.Set(ctx, proxyIPGuardKey(candidate.proxy.Host), state, 0).Err())
			}
			selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
			require.Error(t, err)
			require.Nil(t, selected)
			_, err = a.TransportCoolingDown(ctx, candidate.proxy)
			require.Error(t, err)
		})
	}
}
