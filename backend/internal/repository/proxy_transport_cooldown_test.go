package repository

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func transportCooldownScore(t *testing.T, a *ProxyPoolAllocator, proxy *service.Proxy) int64 {
	t.Helper()
	score, err := a.rdb.ZScore(context.Background(), proxyTransportCooldownKey, proxyTransportCooldownMember(proxy)).Result()
	require.NoError(t, err)
	return int64(score)
}

func TestProxyTransportFirstFailureCoolsGloballyAndReleasesOnlyReportingAccount(t *testing.T) {
	ctx := context.Background()
	a, server := newProxyPoolAllocatorTest(t, 0, poolCandidate(1))
	now := time.Date(2026, 9, 22, 0, 0, 0, 123000000, time.UTC)
	server.SetTime(now)
	for _, id := range []int64{7, 8} {
		_, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: id})
		require.NoError(t, err)
	}
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		return []proxyPoolCandidate{poolCandidate(1), poolCandidate(2)}, nil
	}
	require.NoError(t, a.ReportTransportFailure(ctx, 7, poolCandidate(1).proxy))
	require.Equal(t, now.Add(5*time.Minute).UnixMilli(), transportCooldownScore(t, a, poolCandidate(1).proxy))
	require.Equal(t, 5*time.Minute, a.rdb.PTTL(ctx, proxyTransportCooldownKey).Val())
	require.Zero(t, a.rdb.Exists(ctx, proxyPoolAffinityKey("7")).Val())
	require.Equal(t, "1", a.rdb.HGet(ctx, proxyPoolAffinityKey("8"), "proxy_id").Val())
	require.Equal(t, []string{"8"}, a.rdb.ZRange(ctx, proxyPoolLeaseKey("1"), 0, -1).Val())
	require.Equal(t, []string{"8"}, a.rdb.SMembers(ctx, proxyPoolBoundAccountsKey("1")).Val())
	for _, id := range []int64{7, 8, 9} {
		selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: id})
		require.NoError(t, err)
		require.NotNil(t, selected)
		require.EqualValues(t, 2, selected.ID)
	}
}

func TestProxyTransportRepeatedFailureAndLateSuccessKeepOriginalDeadline(t *testing.T) {
	ctx := context.Background()
	a, server := newProxyPoolAllocatorTest(t, 0, poolCandidate(1))
	proxy := poolCandidate(1).proxy
	now := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	server.SetTime(now)
	require.NoError(t, a.ReportTransportFailure(ctx, 7, proxy))
	server.FastForward(time.Minute)
	server.SetTime(now.Add(time.Minute))
	require.NoError(t, a.ReportTransportFailure(ctx, 8, proxy))
	require.NoError(t, a.ReportSuccess(ctx, 7, proxy.ID))
	require.Equal(t, now.Add(5*time.Minute).UnixMilli(), transportCooldownScore(t, a, proxy))
	require.Equal(t, 4*time.Minute, a.rdb.PTTL(ctx, proxyTransportCooldownKey).Val())
	cooling, err := a.TransportCoolingDown(ctx, proxy)
	require.NoError(t, err)
	require.True(t, cooling)
	server.SetTime(now.Add(5 * time.Minute))
	cooling, err = a.TransportCoolingDown(ctx, proxy)
	require.NoError(t, err)
	require.False(t, cooling, "期限到达即解除，不能依赖全局 ZSET 的 TTL")
	require.NoError(t, a.ReportTransportFailure(ctx, 7, proxy))
	require.Equal(t, now.Add(10*time.Minute).UnixMilli(), transportCooldownScore(t, a, proxy))
}

func TestProxyTransportOldEndpointCannotCoolOrUnbindUpdatedEndpoint(t *testing.T) {
	ctx := context.Background()
	old := poolCandidate(1).proxy
	old.Protocol, old.Host, old.Port = "http", "old.example", 8080
	current := *old
	current.Host = "new.example"
	a, _ := newProxyPoolAllocatorTest(t, 0, proxyPoolCandidate{proxy: &current})
	_, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	affinity := a.rdb.HGetAll(ctx, proxyPoolAffinityKey("7")).Val()
	require.NoError(t, a.ReportTransportFailure(ctx, 7, old))
	require.Equal(t, affinity, a.rdb.HGetAll(ctx, proxyPoolAffinityKey("7")).Val())
	require.Equal(t, []string{"7"}, a.rdb.ZRange(ctx, proxyPoolLeaseKey("1"), 0, -1).Val())
	require.Equal(t, []string{"7"}, a.rdb.SMembers(ctx, proxyPoolBoundAccountsKey("1")).Val())
	cooling, err := a.TransportCoolingDown(ctx, &current)
	require.NoError(t, err)
	require.False(t, cooling)
	cooling, err = a.TransportCoolingDown(ctx, old)
	require.NoError(t, err)
	require.True(t, cooling)
	require.NoError(t, a.ReportTransportFailure(ctx, 7, &current))
	require.NoError(t, a.ReportSuccess(ctx, 7, old.ID))
	cooling, err = a.TransportCoolingDown(ctx, &current)
	require.NoError(t, err)
	require.True(t, cooling, "迟到成功不能解除当前出口的传输冷却")
}

func TestProxyTransportRecoveryRotatesEarliestAndClearsOnlyChosen(t *testing.T) {
	ctx := context.Background()
	a, server := newProxyPoolAllocatorTest(t, 0, poolCandidate(1), poolCandidate(2), poolCandidate(3))
	now := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	for id := int64(1); id <= 3; id++ {
		server.SetTime(now.Add(time.Duration(id) * time.Second))
		require.NoError(t, a.ReportTransportFailure(ctx, 0, poolCandidate(id).proxy))
	}
	for id := int64(1); id <= 3; id++ {
		selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
		require.NoError(t, err)
		require.NotNil(t, selected)
		require.Equal(t, id, selected.ID)
		require.EqualValues(t, 2, a.rdb.ZCard(ctx, proxyTransportCooldownKey).Val())
		cooling, err := a.TransportCoolingDown(ctx, selected)
		require.NoError(t, err)
		require.False(t, cooling)
		server.SetTime(now.Add(time.Duration(10+id) * time.Second))
		require.NoError(t, a.ReportTransportFailure(ctx, 7, selected))
		require.EqualValues(t, 3, a.rdb.ZCard(ctx, proxyTransportCooldownKey).Val())
	}
}

func TestProxyTransportRecoveryUsesBothCooldownDeadlines(t *testing.T) {
	ctx := context.Background()
	a, _ := newProxyPoolAllocatorTest(t, 0, poolCandidate(1), poolCandidate(2))
	seedPoolCooldowns(t, a, map[int64]int64{1: 10, 2: 20})
	require.NoError(t, a.ReportTransportFailure(ctx, 0, poolCandidate(1).proxy))
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.EqualValues(t, 2, selected.ID, "代理 1 的全局冷却比账号冷却更晚，应先恢复代理 2")
	require.Equal(t, []string{"1"}, a.rdb.ZRange(ctx, proxyPoolFailureKey("7"), 0, -1).Val())
	require.NoError(t, a.ReportTransportFailure(ctx, 7, selected))
	_, err = a.rdb.ZAdd(ctx, proxyPoolFailureKey("7"), redis.Z{Member: "2", Score: float64(time.Now().Add(time.Hour).Unix())}).Result()
	require.NoError(t, err)
	selected, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.EqualValues(t, 1, selected.ID)
	require.Equal(t, []string{"2"}, a.rdb.ZRange(ctx, proxyPoolFailureKey("7"), 0, -1).Val())
	require.Equal(t, []string{proxyTransportCooldownMember(poolCandidate(2).proxy)}, a.rdb.ZRange(ctx, proxyTransportCooldownKey, 0, -1).Val())
}

func TestProxyTransportRecoveryPreservesCapacityAndNonCoolingCandidates(t *testing.T) {
	for _, scenario := range []string{"earliest_full", "all_full", "noncooling_full"} {
		t.Run(scenario, func(t *testing.T) {
			ctx := context.Background()
			first, second := poolCandidate(1, "100"), poolCandidate(2)
			if scenario != "earliest_full" {
				second.fixedIDs = []string{"101"}
			}
			if scenario == "noncooling_full" {
				first.fixedIDs = nil
			}
			a, server := newProxyPoolAllocatorTest(t, 1, first, second)
			now := time.Now()
			server.SetTime(now)
			require.NoError(t, a.ReportTransportFailure(ctx, 0, first.proxy))
			server.SetTime(now.Add(time.Second))
			if scenario != "noncooling_full" {
				require.NoError(t, a.ReportTransportFailure(ctx, 0, second.proxy))
			}
			selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
			require.NoError(t, err)
			if scenario == "earliest_full" {
				require.NotNil(t, selected)
				require.EqualValues(t, 2, selected.ID)
			} else {
				require.Nil(t, selected)
			}
			cooling, err := a.TransportCoolingDown(ctx, first.proxy)
			require.NoError(t, err)
			require.True(t, cooling, "容量限制不能解除未选中代理的冷却")
		})
	}
}

func TestProxyTransportConcurrentRecoveryPreservesGlobalCapacity(t *testing.T) {
	ctx := context.Background()
	a, server := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2))
	now := time.Now()
	server.SetTime(now)
	require.NoError(t, a.ReportTransportFailure(ctx, 0, poolCandidate(1).proxy))
	server.SetTime(now.Add(time.Second))
	require.NoError(t, a.ReportTransportFailure(ctx, 0, poolCandidate(2).proxy))
	results, failures := make(chan *service.Proxy, 16), make(chan error, 16)
	var wait sync.WaitGroup
	for id := int64(1); id <= 16; id++ {
		wait.Go(func() {
			selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: id})
			results <- selected
			failures <- err
		})
	}
	wait.Wait()
	close(results)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	count := 0
	for selected := range results {
		if selected != nil {
			count++
			require.EqualValues(t, 1, selected.ID)
		}
	}
	require.Equal(t, 1, count)
	require.EqualValues(t, 1, a.rdb.ZCard(ctx, proxyTransportCooldownKey).Val())
	require.EqualValues(t, 1, a.rdb.ZCard(ctx, proxyPoolLeaseKey("1")).Val())
	require.Zero(t, a.rdb.ZCard(ctx, proxyPoolLeaseKey("2")).Val())
}

func TestProxyTransportReportDoesNotClearDifferentProxyAffinity(t *testing.T) {
	ctx := context.Background()
	a, _ := newProxyPoolAllocatorTest(t, 0, poolCandidate(2))
	_, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.NoError(t, a.ReportTransportFailure(ctx, 7, poolCandidate(1).proxy))
	require.Equal(t, "2", a.rdb.HGet(ctx, proxyPoolAffinityKey("7"), "proxy_id").Val())
	require.Equal(t, []string{strconv.Itoa(7)}, a.rdb.ZRange(ctx, proxyPoolLeaseKey("2"), 0, -1).Val())
}
