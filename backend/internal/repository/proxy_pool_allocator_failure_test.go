package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyPoolAllocatorConnectionFailureExcludesOnlyAffectedAccount(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, poolCandidate(1))
	ctx := context.Background()
	now := time.Now()
	server.SetTime(now)
	_, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	for range 3 {
		require.NoError(t, a.ReportFailure(ctx, 7, 1))
	}
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.Nil(t, selected)
	other, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 8})
	require.NoError(t, err)
	require.NotNil(t, other)
	server.SetTime(now.Add(proxyPoolFailureTTL))
	selected, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.NotNil(t, selected)
}

func TestProxyPoolAllocatorLateFailurePreservesNewBinding(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2))
	ctx := context.Background()
	first, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	for range 3 {
		require.NoError(t, a.ReportFailure(ctx, 7, first.ID))
	}
	second, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	require.NoError(t, a.ReportFailure(ctx, 7, first.ID))
	third, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.Equal(t, second.ID, third.ID)
}

func TestProxyPoolAllocatorSingleFailurePreservesHealthyBinding(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2))
	ctx := context.Background()
	first, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, a.ReportFailure(ctx, 7, first.ID))
		selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
		require.NoError(t, err)
		require.Equal(t, first.ID, selected.ID)
	}
	require.NoError(t, a.ReportFailure(ctx, 7, first.ID))
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, selected.ID)
}

func TestProxyPoolAllocatorSuccessAndWindowResetFailureStreak(t *testing.T) {
	for _, reset := range []string{"success", "window"} {
		t.Run(reset, func(t *testing.T) {
			a, server := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2))
			ctx := context.Background()
			now := time.Now()
			server.SetTime(now)
			first, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
			require.NoError(t, err)
			for range 2 {
				require.NoError(t, a.ReportFailure(ctx, 7, first.ID))
			}
			if reset == "success" {
				require.NoError(t, a.ReportSuccess(ctx, 7, first.ID))
			} else {
				server.SetTime(now.Add(proxyPoolFailureWindow))
			}
			require.NoError(t, a.ReportFailure(ctx, 7, first.ID))
			selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
			require.NoError(t, err)
			require.Equal(t, first.ID, selected.ID)
		})
	}
}

func TestProxyPoolAllocatorLateSuccessDoesNotResetNewProxyFailures(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2))
	ctx := context.Background()
	first, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	for range 3 {
		require.NoError(t, a.ReportFailure(ctx, 7, first.ID))
	}
	second, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, a.ReportFailure(ctx, 7, second.ID))
	}
	require.NoError(t, a.ReportSuccess(ctx, 7, first.ID))
	require.NoError(t, a.ReportFailure(ctx, 7, second.ID))
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.Nil(t, selected)
}

func TestProxyPoolAllocatorConcurrentFailuresCountAtomically(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1))
	ctx := context.Background()
	_, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	var wait sync.WaitGroup
	errors := make(chan error, 16)
	for range 16 {
		wait.Go(func() { errors <- a.ReportFailure(ctx, 7, 1) })
	}
	wait.Wait()
	close(errors)
	for err := range errors {
		require.NoError(t, err)
	}
	require.Zero(t, a.rdb.Exists(ctx, proxyPoolAffinityKey("7")).Val())
	require.Zero(t, a.rdb.ZCard(ctx, proxyPoolLeaseKey("1")).Val())
	require.EqualValues(t, 1, a.rdb.ZCard(ctx, proxyPoolFailureKey("7")).Val())
}
