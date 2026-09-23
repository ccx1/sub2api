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

func seedPoolCooldowns(t *testing.T, a *ProxyPoolAllocator, seconds map[int64]int64) {
	t.Helper()
	ctx := context.Background()
	now, err := a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	entries := make([]redis.Z, 0, len(seconds))
	for id, offset := range seconds {
		entries = append(entries, redis.Z{Member: strconv.FormatInt(id, 10), Score: float64(now.Unix() + offset)})
	}
	require.NoError(t, a.rdb.ZAdd(ctx, proxyPoolFailureKey("7"), entries...).Err())
}

func TestProxyPoolCooldownRecoveryRotatesEarliestOneAtATime(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2), poolCandidate(3))
	seedPoolCooldowns(t, a, map[int64]int64{1: 30, 2: 10, 3: 20})
	ctx := context.Background()
	for _, expected := range []int64{2, 3, 1} {
		selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
		require.NoError(t, err)
		require.NotNil(t, selected, "全部代理冷却时应逐个恢复最早候选")
		require.Equal(t, expected, selected.ID)
		require.EqualValues(t, 2, a.rdb.ZCard(ctx, proxyPoolFailureKey("7")).Val())
		_, err = a.rdb.ZScore(ctx, proxyPoolFailureKey("7"), strconv.FormatInt(expected, 10)).Result()
		require.ErrorIs(t, err, redis.Nil)
		for range proxyPoolFailureThreshold {
			require.NoError(t, a.ReportFailure(ctx, 7, selected.ID))
		}
		require.EqualValues(t, 3, a.rdb.ZCard(ctx, proxyPoolFailureKey("7")).Val())
	}
}

func TestProxyPoolCooldownRecoveryPreservesCapacityAndNonCoolingCandidates(t *testing.T) {
	for _, scenario := range []string{"earliest_full", "all_full", "noncooling_full"} {
		t.Run(scenario, func(t *testing.T) {
			first, second := poolCandidate(1, "100"), poolCandidate(2)
			if scenario != "earliest_full" {
				second.fixedIDs = []string{"101"}
			}
			if scenario == "noncooling_full" {
				first.fixedIDs = nil
			}
			a, _ := newProxyPoolAllocatorTest(t, 1, first, second)
			cooldowns := map[int64]int64{1: 10, 2: 20}
			if scenario == "noncooling_full" {
				delete(cooldowns, 2)
			}
			seedPoolCooldowns(t, a, cooldowns)
			ctx := context.Background()
			selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
			require.NoError(t, err)
			if scenario == "earliest_full" {
				require.NotNil(t, selected)
				require.EqualValues(t, 2, selected.ID)
				require.EqualValues(t, 1, a.rdb.ZCard(ctx, proxyPoolFailureKey("7")).Val())
			} else {
				require.Nil(t, selected)
				require.EqualValues(t, len(cooldowns), a.rdb.ZCard(ctx, proxyPoolFailureKey("7")).Val())
			}
			_, err = a.rdb.ZScore(ctx, proxyPoolFailureKey("7"), "1").Result()
			require.NoError(t, err, "未选中的冷却必须保留")
		})
	}
}

func TestProxyPoolCooldownRecoveryDoesNotResurrectExcludedCandidates(t *testing.T) {
	ctx := context.Background()
	wrongCountry, unhealthy, disabled, allowed := poolCandidate(1), poolCandidate(2), poolCandidate(3), poolCandidate(4)
	disabled.proxy.Status = service.StatusDisabled
	a, _ := newProxyPoolAllocatorTest(t, 0, wrongCountry, unhealthy, disabled, allowed)
	for id, country := range map[int64]string{1: "US", 2: "PH", 3: "PH", 4: "PH"} {
		require.NoError(t, a.latencyCache.SetProxyLatency(ctx, id, &service.ProxyLatencyInfo{
			Success: id != 2, CountryCode: country, UpdatedAt: time.Now(),
		}))
	}
	seedPoolCooldowns(t, a, map[int64]int64{1: 1, 2: 2, 3: 3, 4: 40, 99: 1})
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7, CountryCode: "PH"})
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.EqualValues(t, 4, selected.ID)
	remaining, err := a.rdb.ZRange(ctx, proxyPoolFailureKey("7"), 0, -1).Result()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"1", "2", "3", "99"}, remaining)
}

func TestProxyPoolCooldownRecoveryConcurrentRequestsRecoverOnlyOne(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2))
	seedPoolCooldowns(t, a, map[int64]int64{1: 10, 2: 20})
	ctx := context.Background()
	results := make(chan *service.Proxy, 16)
	errors := make(chan error, 16)
	var wait sync.WaitGroup
	for range 16 {
		wait.Go(func() {
			selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
			results <- selected
			errors <- err
		})
	}
	wait.Wait()
	close(results)
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	for selected := range results {
		require.NotNil(t, selected)
		require.EqualValues(t, 1, selected.ID)
	}
	require.EqualValues(t, 1, a.rdb.ZCard(ctx, proxyPoolFailureKey("7")).Val())
	require.EqualValues(t, 1, a.rdb.ZCard(ctx, proxyPoolLeaseKey("1")).Val())
	require.Zero(t, a.rdb.ZCard(ctx, proxyPoolLeaseKey("2")).Val())
}
