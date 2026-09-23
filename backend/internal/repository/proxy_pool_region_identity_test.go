package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyPoolRegionKeepsProbeAfterMetadataChange(t *testing.T) {
	ctx := context.Background()
	candidate := poolCandidate(1)
	candidate.proxy.UpdatedAt = time.Now()
	a, _ := newProxyPoolAllocatorTest(t, 0, candidate)
	require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 1, &service.ProxyLatencyInfo{
		Success: true, CountryCode: "PH", UpdatedAt: candidate.proxy.UpdatedAt,
		ProxyIdentity: service.ProxyProbeIdentity(candidate.proxy),
	}))
	candidate.proxy.Name = "renamed after probe"
	candidate.proxy.GroupID = new(int64(9))
	candidate.proxy.ExpiresAt = new(time.Now().Add(time.Hour))
	candidate.proxy.UpdatedAt = candidate.proxy.UpdatedAt.Add(time.Minute)
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 42, CountryCode: "PH"})
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.EqualValues(t, 1, selected.ID)

	candidate.proxy.Host = "new-egress.example"
	selected, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 42, CountryCode: "PH"})
	require.NoError(t, err)
	require.Nil(t, selected, "真实出口变动必须重测")
}

func TestProxyPoolRegionNewCandidateNeedsOwnProbe(t *testing.T) {
	ctx := context.Background()
	a, _ := newProxyPoolAllocatorTest(t, 0)
	newCandidate := poolCandidate(2)
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		return []proxyPoolCandidate{newCandidate}, nil
	}
	selection := service.ProxyPoolSelection{AccountID: 42, CountryCode: "PH"}
	selected, err := a.Select(ctx, selection)
	require.NoError(t, err)
	require.Nil(t, selected)
	require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 2, &service.ProxyLatencyInfo{
		Success: true, CountryCode: "PH", UpdatedAt: time.Now(), ProxyIdentity: service.ProxyProbeIdentity(newCandidate.proxy),
	}))
	selected, err = a.Select(ctx, selection)
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.EqualValues(t, 2, selected.ID)
}
