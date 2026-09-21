package repository

import (
	"context"
	"io"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type proxyPoolEntrySource struct{ allocator *ProxyPoolAllocator }

func (s proxyPoolEntrySource) SelectBalancedProxy(ctx context.Context, selection service.ProxyPoolSelection) (*service.Proxy, error) {
	return s.allocator.Select(ctx, selection)
}

func (proxyPoolEntrySource) SelectRandomActiveProxy(context.Context) (*service.Proxy, error) {
	panic("balanced selection must not fall back to the global pool")
}

func (s proxyPoolEntrySource) ReportRandomProxyFailure(ctx context.Context, accountID, proxyID int64) error {
	return s.allocator.ReportFailure(ctx, accountID, proxyID)
}

func (s proxyPoolEntrySource) ReportRandomProxySuccess(ctx context.Context, accountID, proxyID int64) error {
	return s.allocator.ReportSuccess(ctx, accountID, proxyID)
}

func TestProxyPoolEntryResolveEmptyScopeReleasesActualRedisBinding(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1))
	ctx := context.Background()
	account := &service.Account{ID: 7, Extra: map[string]any{service.ProxyModeExtraKey: service.ProxyModeRandom}}
	source := proxyPoolEntrySource{allocator: a}
	require.NoError(t, service.ResolveRandomProxy(ctx, account, source))
	require.EqualValues(t, 1, *account.ProxyID)
	account.Extra[service.RandomProxyPoolScopeExtraKey] = service.RandomProxyPoolSelected
	account.Extra[service.RandomProxyPoolIDsExtraKey] = []int64{}
	require.ErrorIs(t, service.ResolveRandomProxy(ctx, account, source), service.ErrRandomProxyUnavailable)
	require.Nil(t, account.ProxyID)
	require.Zero(t, a.rdb.Exists(ctx, proxyPoolAffinityKey("7")).Val())
	require.Zero(t, a.rdb.ZCard(ctx, proxyPoolLeaseKey("1")).Val())
}

func TestProxyPoolEntryServiceFailureAndSuccessDriveRedisStreak(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2))
	ctx := context.Background()
	account := &service.Account{ID: 7, Extra: map[string]any{service.ProxyModeExtraKey: service.ProxyModeRandom}}
	source := proxyPoolEntrySource{allocator: a}
	require.NoError(t, service.ResolveRandomProxy(ctx, account, source))
	first := *account.ProxyID
	for range 2 {
		require.True(t, service.ReportRandomProxyTransportFailure(ctx, account, source, io.ErrUnexpectedEOF))
		require.NoError(t, service.ResolveRandomProxy(ctx, account, source))
		require.Equal(t, first, *account.ProxyID)
	}
	require.True(t, service.ReportRandomProxySuccess(ctx, account, source))
	for i := 0; i < 3; i++ {
		require.True(t, service.ReportRandomProxyTransportFailure(ctx, account, source, io.ErrUnexpectedEOF))
		require.NoError(t, service.ResolveRandomProxy(ctx, account, source))
		if i < 2 {
			require.Equal(t, first, *account.ProxyID)
		} else {
			require.NotEqual(t, first, *account.ProxyID)
		}
	}
}
