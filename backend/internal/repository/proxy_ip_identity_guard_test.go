package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyIPGuardRememberedEgressSurvivesMissingHealthCache(t *testing.T) {
	ctx := context.Background()
	candidate := ipGuardCandidate(1, "rotating.example")
	a, server := newProxyPoolAllocatorTest(t, 0, candidate)
	info := &service.ProxyLatencyInfo{Success: true, IPAddress: "203.0.113.10", UpdatedAt: time.Now(), ProxyIdentity: service.ProxyProbeIdentity(candidate.proxy)}
	require.NoError(t, a.latencyCache.SetProxyLatency(ctx, candidate.proxy.ID, info))
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.NotNil(t, selected)
	seedProxyIPGuard(t, a, info.IPAddress, true, 0)
	require.NoError(t, a.rdb.Del(ctx, proxyLatencyKey(candidate.proxy.ID)).Err())
	before := server.Dump()
	cooling, err := a.TransportCoolingDown(ctx, candidate.proxy)
	require.NoError(t, err)
	require.True(t, cooling)
	require.Equal(t, before, server.Dump(), "read-only lookup must preserve the remembered identity")
	selected, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 8})
	require.NoError(t, err)
	require.Nil(t, selected, "missing cache cannot bypass a remembered disabled IP")
}

func TestProxyIPGuardNewObservationReplacesOldAndRejectsStaleObservation(t *testing.T) {
	ctx := context.Background()
	proxy := ipGuardCandidate(1, "rotating.example").proxy
	a, server := newProxyPoolAllocatorTest(t, 0, proxyPoolCandidate{proxy: proxy})
	first := &service.ProxyLatencyInfo{Success: true, IPAddress: "203.0.113.10", UpdatedAt: time.Now(), ProxyIdentity: service.ProxyProbeIdentity(proxy)}
	health := map[int64]*service.ProxyLatencyInfo{proxy.ID: first}
	ips, err := a.proxyIPIdentities(ctx, []*service.Proxy{proxy}, health, true)
	require.NoError(t, err)
	require.Equal(t, first.IPAddress, ips[proxy.ID])
	next := *first
	next.IPAddress, next.UpdatedAt = "203.0.113.11", first.UpdatedAt.Add(time.Second)
	health[proxy.ID] = &next
	ips, err = a.proxyIPIdentities(ctx, []*service.Proxy{proxy}, health, true)
	require.NoError(t, err)
	require.Equal(t, next.IPAddress, ips[proxy.ID])
	before := server.Dump()
	health[proxy.ID] = first
	ips, err = a.proxyIPIdentities(ctx, []*service.Proxy{proxy}, health, true)
	require.NoError(t, err)
	require.Equal(t, next.IPAddress, ips[proxy.ID])
	require.Equal(t, before, server.Dump(), "late old observations must not roll back the egress")
	ips, err = a.proxyIPIdentities(ctx, []*service.Proxy{proxy}, nil, false)
	require.NoError(t, err)
	require.Equal(t, next.IPAddress, ips[proxy.ID])
}

func TestProxyIPGuardIdentityDoesNotLeakAcrossChangedProxyConfiguration(t *testing.T) {
	ctx := context.Background()
	proxy := ipGuardCandidate(1, "rotating.example").proxy
	a, _ := newProxyPoolAllocatorTest(t, 0, proxyPoolCandidate{proxy: proxy})
	info := &service.ProxyLatencyInfo{Success: true, IPAddress: "203.0.113.10", UpdatedAt: time.Now(), ProxyIdentity: service.ProxyProbeIdentity(proxy)}
	_, err := a.proxyIPIdentities(ctx, []*service.Proxy{proxy}, map[int64]*service.ProxyLatencyInfo{proxy.ID: info}, true)
	require.NoError(t, err)
	changed := *proxy
	changed.Password = "new-password"
	ips, err := a.proxyIPIdentities(ctx, []*service.Proxy{&changed}, map[int64]*service.ProxyLatencyInfo{proxy.ID: info}, true)
	require.NoError(t, err)
	require.Empty(t, ips[proxy.ID], "neither history nor cached observation matches the changed egress configuration")
}

func TestProxyIPGuardReadOnlyLookupDoesNotRememberNewObservation(t *testing.T) {
	ctx := context.Background()
	proxy := ipGuardCandidate(1, "rotating.example").proxy
	a, server := newProxyPoolAllocatorTest(t, 0, proxyPoolCandidate{proxy: proxy})
	info := &service.ProxyLatencyInfo{Success: true, IPAddress: "203.0.113.10", UpdatedAt: time.Now(), ProxyIdentity: service.ProxyProbeIdentity(proxy)}
	require.NoError(t, a.latencyCache.SetProxyLatency(ctx, proxy.ID, info))
	before := server.Dump()
	ips, err := a.proxyIPIdentities(ctx, []*service.Proxy{proxy}, map[int64]*service.ProxyLatencyInfo{proxy.ID: info}, false)
	require.NoError(t, err)
	require.Equal(t, info.IPAddress, ips[proxy.ID])
	require.Equal(t, before, server.Dump())
}
