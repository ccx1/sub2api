package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyPoolAllocatorPrefersHealthyIdleThenUnknownThenOverlap(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, poolCandidate(1), poolCandidate(2), poolCandidate(3, "100"))
	ctx := context.Background()
	for _, id := range []int64{1, 3} {
		require.NoError(t, a.latencyCache.SetProxyLatency(ctx, id, &service.ProxyLatencyInfo{
			Success: true, QualityStatus: "healthy", QualityGrade: "A", UpdatedAt: time.Now(),
		}))
	}
	first, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 1})
	require.NoError(t, err)
	require.EqualValues(t, 1, first.ID)
	second, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 2})
	require.NoError(t, err)
	require.EqualValues(t, 2, second.ID, "an unknown idle proxy should precede further overlap")
}

func TestProxyPoolAllocatorHealthFiltersAndDegrades(t *testing.T) {
	for _, status := range []string{"failed", "challenge"} {
		t.Run(status, func(t *testing.T) {
			a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2), poolCandidate(3, "100"))
			ctx := context.Background()
			require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 1, &service.ProxyLatencyInfo{Success: false, UpdatedAt: time.Now()}))
			require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 2, &service.ProxyLatencyInfo{
				Success: true, QualityStatus: status, UpdatedAt: time.Now(),
			}))
			proxy, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
			require.NoError(t, err)
			require.NotNil(t, proxy)
			require.EqualValues(t, 2, proxy.ID, "degraded but connected proxy is a fallback when normal capacity is exhausted")
			proxy, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 8})
			require.NoError(t, err)
			require.Nil(t, proxy, "a known network failure must never become a fallback")
		})
	}
}

func TestProxyPoolAllocatorAvoidsDegradedUntilNormalCapacityExhausted(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 2, poolCandidate(1), poolCandidate(2, "100"))
	ctx := context.Background()
	require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 1, &service.ProxyLatencyInfo{
		Success: true, QualityStatus: "challenge", UpdatedAt: time.Now(),
	}))
	proxy, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.EqualValues(t, 2, proxy.ID)
}

func TestProxyPoolAllocatorDegradedBindingYieldsOnlyToAvailableNormalProxy(t *testing.T) {
	for _, status := range []string{"failed", "challenge"} {
		for _, available := range []bool{true, false} {
			t.Run(status+"/normal_available="+strconv.FormatBool(available), func(t *testing.T) {
				a, server := newProxyPoolAllocatorTest(t, 1, poolCandidate(1))
				ctx := context.Background()
				now := time.Now()
				server.SetTime(now)
				selection := service.ProxyPoolSelection{AccountID: 7, MaxReuseDuration: time.Hour}
				first, err := a.Select(ctx, selection)
				require.NoError(t, err)
				require.EqualValues(t, 1, first.ID)
				server.SetTime(now.Add(30 * time.Minute))
				require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 1, &service.ProxyLatencyInfo{
					Success: true, QualityStatus: status, UpdatedAt: time.Now(),
				}))
				require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 2, &service.ProxyLatencyInfo{
					Success: true, QualityStatus: "healthy", UpdatedAt: time.Now(),
				}))
				other := poolCandidate(2)
				if !available {
					other.fixedIDs = []string{"100"}
				}
				a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
					return []proxyPoolCandidate{poolCandidate(1), other}, nil
				}
				selected, err := a.Select(ctx, selection)
				require.NoError(t, err)
				require.NotNil(t, selected)
				if available {
					require.EqualValues(t, 2, selected.ID, "degraded affinity must yield to a normal proxy with capacity")
					require.Zero(t, a.rdb.ZCard(ctx, proxyPoolLeaseKey("1")).Val())
					server.SetTime(now.Add(time.Hour))
					next, err := a.Select(ctx, selection)
					require.NoError(t, err)
					require.EqualValues(t, 2, next.ID, "a replacement starts a new reuse period")
				} else {
					require.EqualValues(t, 1, selected.ID, "the degraded binding remains a fallback when normal capacity is exhausted")
				}
			})
		}
	}
}

func TestProxyPoolAllocatorIgnoresProbeFromBeforeProxyEdit(t *testing.T) {
	candidate := poolCandidate(1)
	candidate.proxy.UpdatedAt = time.Now()
	a, _ := newProxyPoolAllocatorTest(t, 1, candidate)
	ctx := context.Background()
	require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 1, &service.ProxyLatencyInfo{
		Success: false, UpdatedAt: candidate.proxy.UpdatedAt.Add(-time.Second),
	}))
	proxy, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.NotNil(t, proxy)
}

func TestProxyPoolAllocatorRechecksExpiryBeforeReservation(t *testing.T) {
	candidate := poolCandidate(1)
	expires := time.Now().Add(-time.Second)
	candidate.proxy.ExpiresAt = &expires
	a, _ := newProxyPoolAllocatorTest(t, 1, candidate)
	proxy, err := a.Select(context.Background(), service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.Nil(t, proxy)
}
