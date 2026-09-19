package repository

import (
	"context"
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
	require.NoError(t, a.ReportFailure(ctx, 7, 1))
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
	require.NoError(t, a.ReportFailure(ctx, 7, first.ID))
	second, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	require.NoError(t, a.ReportFailure(ctx, 7, first.ID))
	third, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.Equal(t, second.ID, third.ID)
}
