package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexSchedulerPoolFiltersAndValidCandidateAfterRemoval(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2), codexPoolCandidate(3))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	cfg.Protection.Enabled = false
	req := codexRequest(7, cfg)
	req.Selection.Restricted, req.Selection.IDs = true, []int64{2}
	r := codexStart(t, a, req)
	require.EqualValues(t, 2, r.Proxy.ID)
	codexFinish(t, a, r, "success")
	req.Selection.IDs = []int64{}
	_, err := a.ReserveCodexTicket(ctx, req)
	requireCodexWait(t, err, "pool_empty")
	req.Selection.Restricted = false
	r = codexStart(t, a, req)
	last := r.Proxy.ID
	codexFinish(t, a, r, "success")
	all := []proxyPoolCandidate{codexPoolCandidate(1), codexPoolCandidate(2), codexPoolCandidate(3)}
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		result := []proxyPoolCandidate{}
		for _, c := range all {
			if c.proxy.ID != last {
				result = append(result, c)
			}
		}
		return result, nil
	}
	r = codexStart(t, a, req)
	require.NotEqual(t, last, r.Proxy.ID)
	require.Contains(t, []int64{1, 2, 3}, r.Proxy.ID)
	require.True(t, r.Proxy.IsActive())
	require.False(t, r.Proxy.IsExpired(time.Now()))
	require.Equal(t, codexPoolCandidate(r.Proxy.ID).proxy, r.Proxy)
	codexFinish(t, a, r, "success")
}

func TestCodexSchedulerPoolRecoveryReservationDoesNotConsume(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	req := codexRequest(7, cfg)
	r := codexStart(t, a, codexRequest(8, cfg))
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, false, true, false))
	codexFinish(t, a, r, "ticket_rejected")
	for range 3 {
		reserved, err := a.ReserveCodexTicket(ctx, req)
		require.NoError(t, err)
		require.True(t, reserved.HalfOpen)
		require.Zero(t, reserved.Status.AttemptsUsed)
		require.Equal(t, 1, reserved.Status.Round)
		codexFinish(t, a, reserved, "canceled")
	}
	codexAdvance(t, a, server, 6*time.Second)
	canceled, err := a.ReserveCodexTicket(ctx, req)
	require.NoError(t, err)
	codexFinish(t, a, canceled, "canceled")
	require.Equal(t, 1, canceled.Status.Round)
	codexAdvance(t, a, server, 2*time.Second)
	r = codexStart(t, a, req)
	require.True(t, r.HalfOpen)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	codexFinish(t, a, r, "success")
	next, err := a.ReserveCodexTicket(ctx, req)
	require.NoError(t, err)
	require.Zero(t, next.Status.AttemptsUsed)
	require.Equal(t, 1, next.Status.Round)
	require.Equal(t, r.Proxy.ID, next.Proxy.ID)
	codexFinish(t, a, next, "canceled")
}

func TestCodexSchedulerModelFairnessAndCanceledReservationDoesNotTakeTurn(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	cfg.Protection.Enabled = false
	req := codexRequest(7, cfg)
	req.Models = []string{"sol", "astra", "sol"}
	req.Model = "sol"
	_, err := a.ReserveCodexTicket(ctx, req)
	status := requireCodexWait(t, err, "model_turn")
	require.Equal(t, "astra", status.Model)
	req.Model = "astra"
	r, err := a.ReserveCodexTicket(ctx, req)
	require.NoError(t, err)
	codexFinish(t, a, r, "canceled")
	r = codexStart(t, a, req)
	codexFinish(t, a, r, "success")
	_, err = a.ReserveCodexTicket(ctx, req)
	require.Equal(t, "sol", requireCodexWait(t, err, "model_turn").Model)
	req.Model = "sol"
	r = codexStart(t, a, req)
	codexFinish(t, a, r, "success")
}

func TestCodexSchedulerLeaseTTLDoesNotShortenOtherAccountsReservation(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	longCfg := cfg
	longCfg.HarvestAttemptTimeoutSeconds = 60
	long, err := a.ReserveCodexTicket(ctx, codexRequest(1, longCfg))
	require.NoError(t, err)
	short, err := a.ReserveCodexTicket(ctx, codexRequest(2, cfg))
	require.NoError(t, err)
	codexAdvance(t, a, server, 35*time.Second)
	require.True(t, a.rdb.ZScore(ctx, "proxy:{pool}:codex:leases:1", "1").Val() > 0)
	require.NoError(t, a.StartCodexTicket(ctx, long))
	require.Error(t, a.StartCodexTicket(ctx, short))
	codexFinish(t, a, long, "success")
}

func TestCodexSchedulerDisabledProtectionStillHonorsExistingSilenceUntilDeadline(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	r := codexStart(t, a, codexRequest(1, cfg))
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, false, true, false))
	codexFinish(t, a, r, "ticket_rejected")
	cfg.Protection.Enabled = false
	_, err := a.ReserveCodexTicket(ctx, codexRequest(2, cfg))
	requireCodexWait(t, err, "proxy_silent")
	codexAdvance(t, a, server, 6*time.Second)
	r = codexStart(t, a, codexRequest(2, cfg))
	require.False(t, r.HalfOpen)
	codexFinish(t, a, r, "success")
}
