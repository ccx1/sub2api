package repository

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type proxyPoolSettingsStub struct {
	limit int
	err   error
}

func (s proxyPoolSettingsStub) GetProxyPoolMaxAccounts(context.Context) (int, error) {
	return s.limit, s.err
}

func newProxyPoolAllocatorTest(t *testing.T, limit int, candidates ...proxyPoolCandidate) (*ProxyPoolAllocator, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = rdb.Close() })
	a := &ProxyPoolAllocator{rdb: rdb, latencyCache: NewProxyLatencyCache(rdb), settings: proxyPoolSettingsStub{limit: limit}}
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		return append([]proxyPoolCandidate{}, candidates...), nil
	}
	return a, server
}

func poolCandidate(id int64, fixed ...string) proxyPoolCandidate {
	return proxyPoolCandidate{proxy: &service.Proxy{ID: id, Status: service.StatusActive}, fixedIDs: fixed}
}

func TestProxyPoolAllocatorFillsIdleProxiesAndBalancesOverlap(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, poolCandidate(1, "100", "101"), poolCandidate(2), poolCandidate(3))
	ctx := context.Background()
	counts := map[int64]int{1: 2, 2: 0, 3: 0}
	for accountID := int64(1); accountID <= 10; accountID++ {
		selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: accountID})
		require.NoError(t, err)
		require.NotNil(t, selected)
		if accountID <= 2 {
			require.NotEqualValues(t, 1, selected.ID, "first allocate entirely idle proxies")
			require.Zero(t, counts[selected.ID])
		}
		counts[selected.ID]++
	}
	require.Equal(t, map[int64]int{1: 4, 2: 4, 3: 4}, counts)
}

func TestProxyPoolAllocatorConcurrentCapacityIsAtomic(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 2, poolCandidate(1), poolCandidate(2), poolCandidate(3), poolCandidate(4))
	ctx := context.Background()
	results := make(chan *service.Proxy, 40)
	failures := make(chan error, 40)
	var wg sync.WaitGroup
	for id := int64(1); id <= 40; id++ {
		wg.Go(func() {
			proxy, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: id})
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
	counts := make(map[int64]int)
	for proxy := range results {
		if proxy != nil {
			counts[proxy.ID]++
		}
	}
	require.Equal(t, map[int64]int{1: 2, 2: 2, 3: 2, 4: 2}, counts)
}

func TestProxyPoolAllocatorDeduplicatesFixedAndDynamicAccount(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 2, poolCandidate(1, "7"))
	ctx := context.Background()
	for range 3 {
		selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
		require.NoError(t, err)
		require.NotNil(t, selected)
	}
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 8})
	require.NoError(t, err)
	require.NotNil(t, selected, "the fixed account and its ticket jobs only occupy one place")
	selected, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 9})
	require.NoError(t, err)
	require.Nil(t, selected)
	require.EqualValues(t, 2, a.rdb.ZCard(ctx, proxyPoolLeaseKey("1")).Val())
}

func TestProxyPoolAllocatorSameAccountReusesHealthyProxy(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2))
	ctx := context.Background()
	first, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	second, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	for range 5 {
		selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
		require.NoError(t, err)
		require.NotNil(t, selected)
		require.Equal(t, first.ID, selected.ID)
	}
	require.EqualValues(t, 1, a.rdb.ZCard(ctx, proxyPoolLeaseKey(strconv.FormatInt(first.ID, 10))).Val())
	require.EqualValues(t, 0, a.rdb.ZCard(ctx, proxyPoolLeaseKey(strconv.FormatInt(3-first.ID, 10))).Val())
}

func TestProxyPoolAllocatorLeaseExpiryReleasesCapacity(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 1, poolCandidate(1))
	ctx := context.Background()
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.NotNil(t, selected)
	server.FastForward(proxyPoolLeaseTTL + time.Second)
	selected, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 8})
	require.NoError(t, err)
	require.NotNil(t, selected)
}

func TestProxyPoolAllocatorPrunesExpiredMembersInLiveKey(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 2, poolCandidate(1))
	ctx := context.Background()
	now, err := a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	require.NoError(t, a.rdb.ZAdd(ctx, proxyPoolLeaseKey("1"), redis.Z{Score: float64(now.Unix() - 1), Member: "7"},
		redis.Z{Score: float64(now.Add(proxyPoolLeaseTTL).Unix()), Member: "8"}).Err())
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 9})
	require.NoError(t, err)
	require.NotNil(t, selected)
	members, err := a.rdb.ZRange(ctx, proxyPoolLeaseKey("1"), 0, -1).Result()
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"8", "9"}, members)
}

func TestProxyPoolAllocatorEmptyScopeAndDependencyFailures(t *testing.T) {
	ctx := context.Background()
	var unavailable *ProxyPoolAllocator
	selected, err := unavailable.Select(ctx, service.ProxyPoolSelection{Restricted: true})
	require.NoError(t, err)
	require.Nil(t, selected)
	_, err = unavailable.Select(ctx, service.ProxyPoolSelection{})
	require.Error(t, err)
	a, _ := newProxyPoolAllocatorTest(t, 0)
	selected, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 1})
	require.NoError(t, err)
	require.Nil(t, selected)
	failure := errors.New("database unavailable")
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) { return nil, failure }
	_, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 1})
	require.ErrorIs(t, err, failure)
	a.settings = proxyPoolSettingsStub{err: failure}
	_, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 1})
	require.ErrorIs(t, err, failure)
}

func TestProxyPoolAllocatorRedisFailureNeverBypassesCapacity(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1))
	require.NoError(t, a.rdb.Close())
	selected, err := a.Select(context.Background(), service.ProxyPoolSelection{AccountID: 7})
	require.Error(t, err)
	require.Nil(t, selected)
}

func TestProxyPoolAllocatorReservationFailureNeverReturnsUnreservedProxy(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1))
	ctx := context.Background()
	require.NoError(t, a.rdb.Set(ctx, proxyPoolLeaseKey("1"), "invalid lease data", 0).Err())
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.ErrorContains(t, err, "reserve proxy pool association")
	require.Nil(t, selected)
}

func TestProxyPoolAllocatorAnonymousRequestsStillReserveCapacity(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1))
	selected, err := a.Select(context.Background(), service.ProxyPoolSelection{})
	require.NoError(t, err)
	require.NotNil(t, selected)
	selected, err = a.Select(context.Background(), service.ProxyPoolSelection{})
	require.NoError(t, err)
	require.Nil(t, selected)
}

func TestProxyPoolAllocatorAllTiesCanBeSelected(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, poolCandidate(1), poolCandidate(2), poolCandidate(3))
	ctx := context.Background()
	for id := int64(1); id <= 3; id++ {
		proxy, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: id})
		require.NoError(t, err)
		require.NotNil(t, proxy)
	}
	for id := 1; id <= 3; id++ {
		require.EqualValues(t, 1, a.rdb.ZCard(ctx, proxyPoolLeaseKey(strconv.Itoa(id))).Val())
	}
}
