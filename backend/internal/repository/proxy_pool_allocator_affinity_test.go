package repository

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyPoolAllocatorConcurrentSameAccountKeepsOneAssociation(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2), poolCandidate(3))
	ctx := context.Background()
	results := make(chan *service.Proxy, 32)
	failures := make(chan error, 32)
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			// 独立 allocator 共享 Redis，模拟多个服务进程。
			worker := *a
			proxy, err := worker.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
			results <- proxy
			failures <- err
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	selected := int64(0)
	for proxy := range results {
		require.NotNil(t, proxy)
		if selected == 0 {
			selected = proxy.ID
		}
		require.Equal(t, selected, proxy.ID)
	}
	total := int64(0)
	for _, id := range []string{"1", "2", "3"} {
		total += a.rdb.ZCard(ctx, proxyPoolLeaseKey(id)).Val()
	}
	require.EqualValues(t, 1, total)
}

func TestProxyPoolAllocatorAffinitySurvivesIdleLeaseExpiry(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, poolCandidate(1), poolCandidate(2))
	ctx := context.Background()
	first, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	server.FastForward(24 * time.Hour)
	second, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
}

func TestProxyPoolAllocatorHealthyBindingPrecedesIdleOrBetterProxy(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, poolCandidate(1))
	ctx := context.Background()
	first, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		return []proxyPoolCandidate{poolCandidate(1, "100", "101"), poolCandidate(2)}, nil
	}
	require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 2, &service.ProxyLatencyInfo{
		Success: true, QualityStatus: "healthy", QualityGrade: "A", UpdatedAt: time.Now(),
	}))
	second, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
}

func TestProxyPoolAllocatorRotatesAtConfiguredPeriod(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2))
	ctx := context.Background()
	now := time.Date(2026, 9, 19, 8, 0, 0, 0, time.UTC)
	server.SetTime(now)
	selection := service.ProxyPoolSelection{AccountID: 7, MaxReuseDuration: time.Hour}
	first, err := a.Select(ctx, selection)
	require.NoError(t, err)
	server.SetTime(now.Add(time.Hour - time.Second))
	second, err := a.Select(ctx, selection)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	server.SetTime(now.Add(time.Hour))
	third, err := a.Select(ctx, selection)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, third.ID)
	require.EqualValues(t, 0, a.rdb.ZCard(ctx, proxyPoolLeaseKey(strconv.FormatInt(first.ID, 10))).Val())
	fourth, err := a.Select(ctx, selection)
	require.NoError(t, err)
	require.Equal(t, third.ID, fourth.ID)
}

func TestProxyPoolAllocatorExpiryKeepsOnlyAvailableExit(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2, "100"))
	ctx := context.Background()
	now := time.Now()
	server.SetTime(now)
	selection := service.ProxyPoolSelection{AccountID: 7, MaxReuseDuration: time.Minute}
	first, err := a.Select(ctx, selection)
	require.NoError(t, err)
	server.SetTime(now.Add(time.Minute))
	second, err := a.Select(ctx, selection)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
}

func TestProxyPoolAllocatorUnavailableBindingChangesAndReleasesCapacity(t *testing.T) {
	for _, reason := range []string{"health", "disabled", "expired", "removed", "capacity", "address"} {
		t.Run(reason, func(t *testing.T) {
			candidate := poolCandidate(1)
			a, _ := newProxyPoolAllocatorTest(t, 1, candidate)
			ctx := context.Background()
			_, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
			require.NoError(t, err)
			candidates := []proxyPoolCandidate{candidate, poolCandidate(2)}
			switch reason {
			case "health":
				require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 1, &service.ProxyLatencyInfo{Success: false, UpdatedAt: time.Now()}))
			case "disabled":
				candidate.proxy.Status = service.StatusDisabled
			case "expired":
				expired := time.Now().Add(-time.Second)
				candidate.proxy.ExpiresAt = &expired
			case "removed":
				candidates = candidates[1:]
			case "capacity":
				candidates[0].fixedIDs = []string{"100"}
			case "address":
				candidate.proxy.Host = "updated.example"
			}
			a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
				return candidates, nil
			}
			selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
			require.NoError(t, err)
			require.EqualValues(t, 2, selected.ID)
			require.Zero(t, a.rdb.ZCard(ctx, proxyPoolLeaseKey("1")).Val())
		})
	}
}

func TestProxyPoolAllocatorScopeChangesDoNotRestoreOldBinding(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1)
	a.loadCandidates = func(_ context.Context, selection service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		candidates := make([]proxyPoolCandidate, 0, len(selection.IDs))
		for _, id := range selection.IDs {
			candidates = append(candidates, poolCandidate(id))
		}
		return candidates, nil
	}
	ctx := context.Background()
	for _, ids := range [][]int64{{1}, {2}, {1, 2}} {
		selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7, Restricted: true, IDs: ids})
		require.NoError(t, err)
		require.Equal(t, ids[len(ids)-1], selected.ID)
	}
	require.Zero(t, a.rdb.ZCard(ctx, proxyPoolLeaseKey("1")).Val())
}

func TestProxyPoolAllocatorEmptyPoolClearsBindingAndCapacity(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1))
	ctx := context.Background()
	_, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) { return nil, nil }
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.Nil(t, selected)
	require.Zero(t, a.rdb.ZCard(ctx, proxyPoolLeaseKey("1")).Val())
	require.Zero(t, a.rdb.Exists(ctx, proxyPoolAffinityKey("7")).Val())
}
