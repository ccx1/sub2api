package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexSchedulerRegionOnlyReservesMatchingPoolAndFixedProxy(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
	ctx := context.Background()
	for id, country := range map[int64]string{1: "JP", 2: "PH"} {
		require.NoError(t, a.latencyCache.SetProxyLatency(ctx, id, &service.ProxyLatencyInfo{
			Success: true, CountryCode: country, UpdatedAt: time.Now(),
		}))
	}
	req := codexRequest(42, codexSchedulerConfig())
	req.Selection.CountryCode = "JP"
	reserved, err := a.ReserveCodexTicket(ctx, req)
	require.NoError(t, err)
	require.EqualValues(t, 1, reserved.Proxy.ID)
	codexFinish(t, a, reserved, "canceled")
	req.PoolMode, req.FixedProxy = false, codexPoolCandidate(2).proxy
	_, err = a.ReserveCodexTicket(ctx, req)
	requireCodexWait(t, err, "pool_empty")
	req.FixedProxy = nil
	_, err = a.ReserveCodexTicket(ctx, req)
	require.ErrorContains(t, err, "direct ticket egress")
}

func TestCodexSchedulerRegionInvalidatesDeferredProxy(t *testing.T) {
	for _, reason := range []string{"different_country", "unknown_country", "stale_probe"} {
		t.Run(reason, func(t *testing.T) {
			_, client := newAPIKeyRepoSQLite(t)
			a, _ := newProxyPoolAllocatorTest(t, 0)
			a.client = client
			ctx := context.Background()
			proxy := createPoolTestProxy(t, client, "deferred")
			info := &service.ProxyLatencyInfo{Success: true, CountryCode: "PH", UpdatedAt: time.Now()}
			if reason == "unknown_country" {
				info.CountryCode = ""
			} else if reason == "stale_probe" {
				info.CountryCode, info.UpdatedAt = "JP", proxy.UpdatedAt.Add(-time.Second)
			}
			require.NoError(t, a.latencyCache.SetProxyLatency(ctx, proxy.ID, info))
			require.NoError(t, a.rdb.Set(ctx, codexSchedulerAccountKey(42), fmt.Sprintf(`{"deferredproxy":"%d"}`, proxy.ID), 0).Err())
			req := codexRequest(42, codexSchedulerConfig())
			req.Selection.CountryCode = "JP"
			query := map[string]any{}
			require.NoError(t, a.codexSchedulerDeferredProxy(ctx, req, query))
			require.Equal(t, true, query["deferred_invalid"])
			require.NotContains(t, query, "deferred_candidate")
		})
	}
}
