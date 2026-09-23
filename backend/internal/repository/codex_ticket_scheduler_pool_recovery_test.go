package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func silenceCodexPoolProxy(t *testing.T, a *ProxyPoolAllocator, id int64) {
	t.Helper()
	req := codexRequest(100+id, codexSchedulerConfig())
	req.Config.Protection.ProxySilenceSeconds = 300
	req.PoolMode, req.FixedProxy = false, codexPoolCandidate(id).proxy
	codexReject(t, a, codexStart(t, a, req))
}

func TestCodexSchedulerPoolRecoversOldestSilenceOneAtATime(t *testing.T) {
	for _, mode := range []string{"automatic", "manual", "rejection"} {
		t.Run(mode, func(t *testing.T) {
			a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
			ctx := context.Background()
			silenceCodexPoolProxy(t, a, 2)
			codexAdvance(t, a, server, time.Second)
			silenceCodexPoolProxy(t, a, 1)
			req := codexRequest(7, codexSchedulerConfig())
			req.Manual = mode == "manual"
			req.Config.Protection.ProxySilenceSeconds = 300
			if mode == "rejection" {
				req.AccountID, req.Selection.AccountID = 102, 102
				codexAdvance(t, a, server, 30*time.Second)
			}
			first := codexStart(t, a, req)
			require.EqualValues(t, 2, first.Proxy.ID)
			require.True(t, first.HalfOpen)
			other := req
			other.AccountID, other.Selection.AccountID = 8, 8
			_, err := a.ReserveCodexTicket(ctx, other)
			requireCodexWait(t, err, "half_open_busy")
			codexAdvance(t, a, server, time.Second)
			codexReject(t, a, first)
			second := codexStart(t, a, other)
			require.EqualValues(t, 1, second.Proxy.ID, "a failed recovery returns to the end of the cooldown order")
			require.True(t, second.HalfOpen)
			require.NoError(t, a.ReportCodexTicketHarvest(ctx, second, true, false, false))
			codexFinish(t, a, second, "success")
			other.AccountID, other.Selection.AccountID = 9, 9
			next := codexStart(t, a, other)
			require.EqualValues(t, 1, next.Proxy.ID)
			require.False(t, next.HalfOpen, "a recovered healthy proxy takes precedence over another cooling IP")
			codexFinish(t, a, next, "success")
		})
	}
}

func TestCodexSchedulerPoolRecoveryWaitsWhenProbeHealthChanges(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
	ctx := context.Background()
	silenceCodexPoolProxy(t, a, 1)
	silenceCodexPoolProxy(t, a, 2)
	first := codexStart(t, a, codexRequest(7, codexSchedulerConfig()))
	require.NoError(t, a.latencyCache.SetProxyLatency(ctx, first.Proxy.ID, &service.ProxyLatencyInfo{
		Success: false, UpdatedAt: time.Now(),
	}))
	_, err := a.ReserveCodexTicket(ctx, codexRequest(8, codexSchedulerConfig()))
	requireCodexWait(t, err, "half_open_busy")
	codexFinish(t, a, first, "canceled")
}

func TestCodexSchedulerExpiredSilentPinPrefersAvailableProxy(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	req := codexRequest(7, codexSchedulerConfig())
	first := codexStart(t, a, req)
	codexReject(t, a, first)
	codexAdvance(t, a, server, 31*time.Second)
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		return []proxyPoolCandidate{codexPoolCandidate(1), codexPoolCandidate(2)}, nil
	}
	next := codexStart(t, a, req)
	require.EqualValues(t, 2, next.Proxy.ID)
	require.False(t, next.HalfOpen)
	codexFinish(t, a, next, "success")
}

func TestCodexSchedulerManualFallbackRetainsAvoidedHalfOpenCandidate(t *testing.T) {
	fixture := newCodexFallbackTest(t, "harvest")
	fixture.selection.IDs = append(fixture.selection.IDs, 3)
	fixture.block(t, fixture.initial, "silent")
	fixture.block(t, 3, "unhealthy")
	proxy, err := fixture.try(t, true)
	require.NoError(t, err)
	require.Equal(t, fixture.initial, proxy.ID)
}

func TestCodexSchedulerPoolRecoveryIsAtomicAcrossAccounts(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
	silenceCodexPoolProxy(t, a, 1)
	codexAdvance(t, a, server, time.Second)
	silenceCodexPoolProxy(t, a, 2)
	var wg sync.WaitGroup
	results := make(chan *service.CodexTicketReservation, 16)
	failures := make(chan error, 16)
	for id := int64(1); id <= 16; id++ {
		wg.Go(func() {
			r, err := a.ReserveCodexTicket(context.Background(), codexRequest(id, codexSchedulerConfig()))
			if err != nil {
				failures <- err
			} else {
				results <- r
			}
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	require.Len(t, results, 1)
	require.Len(t, failures, 15)
	for err := range failures {
		requireCodexWait(t, err, "half_open_busy")
	}
	for r := range results {
		require.EqualValues(t, 1, r.Proxy.ID)
		codexFinish(t, a, r, "canceled")
	}
}

func TestCodexSchedulerPoolRecoveryPreservesCapacityAndScope(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1, "99"), codexPoolCandidate(2), codexPoolCandidate(3))
	silenceCodexPoolProxy(t, a, 1)
	codexAdvance(t, a, server, time.Second)
	silenceCodexPoolProxy(t, a, 2)
	a.settings = proxyPoolSettingsStub{limit: 1}
	req := codexRequest(7, codexSchedulerConfig())
	req.Selection.Restricted, req.Selection.IDs = true, []int64{1, 2}
	r := codexStart(t, a, req)
	require.EqualValues(t, 2, r.Proxy.ID, "skip capacity-full oldest IP without selecting outside the allowed pool")
	codexFinish(t, a, r, "canceled")
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		return []proxyPoolCandidate{codexPoolCandidate(1, "99"), codexPoolCandidate(2, "99")}, nil
	}
	_, err := a.ReserveCodexTicket(context.Background(), req)
	requireCodexWait(t, err, "capacity")
}

func TestCodexSchedulerNewCandidateJoinsExistingRound(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
	req := codexRequest(7, codexSchedulerConfig())
	req.Config.ProxyFailureThreshold = 1
	first := codexStart(t, a, req)
	codexReject(t, a, first)
	silenceCodexPoolProxy(t, a, 3-first.Proxy.ID)
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		return []proxyPoolCandidate{codexPoolCandidate(1), codexPoolCandidate(2), codexPoolCandidate(3)}, nil
	}
	req.Manual = true
	next := codexStart(t, a, req)
	require.EqualValues(t, 3, next.Proxy.ID)
	require.False(t, next.HalfOpen)
	require.Equal(t, first.Generation, next.Generation, "pool expansion must not reset the account budget")
	codexFinish(t, a, next, "canceled")
}

func TestCodexSchedulerSilentPinDoesNotHideNewAvailableProxy(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	req := codexRequest(7, codexSchedulerConfig())
	req.Config.ProxyFailureThreshold = 20
	first := codexStart(t, a, req)
	codexReject(t, a, first)
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		return []proxyPoolCandidate{codexPoolCandidate(1), codexPoolCandidate(2)}, nil
	}
	req.Manual = true
	next := codexStart(t, a, req)
	require.EqualValues(t, 2, next.Proxy.ID)
	require.False(t, next.HalfOpen)
	codexFinish(t, a, next, "canceled")
}
