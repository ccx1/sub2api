package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func codexReject(t *testing.T, a *ProxyPoolAllocator, r *service.CodexTicketReservation) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, false, true, false))
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
		Reservation: r, Outcome: "ticket_rejected", Silence: true, HarvestProxyFailed: true,
	}))
}

func TestCodexSchedulerRejectionRetryOwnsIntervalBudgetAndCooldown(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	server.SetTime(time.Date(2026, time.September, 22, 0, 0, 0, 0, time.UTC))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	cfg.Protection.ProxySilenceSeconds = 300
	cfg.Protection.MaxAccountAttempts, cfg.Protection.MaxPoolRounds = 1, 1
	cfg.Protection.RejectionRetryIntervalSeconds = 30
	cfg.Protection.RejectionRetryMaxAttempts = 6
	cfg.Protection.RejectionRetryCooldownSeconds = 300
	cfg.RetryBackoffSeconds, cfg.ProxyFailureThreshold = []int{600}, 1
	req := codexRequest(7, cfg)
	for i := 1; i <= 6; i++ {
		req.Model = []string{"astra", "sol"}[i%2]
		req.Models = []string{req.Model}
		r := codexStart(t, a, req)
		now, err := a.rdb.Time(ctx).Result()
		require.NoError(t, err)
		codexReject(t, a, r)
		want, delay := "rejection_retry", 30*time.Second
		if i == 6 {
			want, delay = "rejection_cooldown", 300*time.Second
		}
		require.Equal(t, i, r.Status.AttemptsUsed)
		require.Nil(t, r.Status.CooldownUntil, "ordinary budget cooldown must not override rejection retry")
		require.Equal(t, want, r.Status.Reason)
		require.Equal(t, now.Add(delay).UnixMilli(), r.Status.RetryAt.UnixMilli())
		codexAdvance(t, a, server, delay-time.Second)
		_, err = a.ReserveCodexTicket(ctx, req)
		requireCodexWait(t, err, want)
		codexAdvance(t, a, server, time.Second)
	}
	next := codexStart(t, a, req)
	codexReject(t, a, next)
	require.Equal(t, 1, next.Status.AttemptsUsed)
	require.Equal(t, "rejection_retry", next.Status.Reason)
}

func TestCodexSchedulerSuccessClearsAllFailureBudgets(t *testing.T) {
	for _, outcome := range []string{"verified", "published", "success"} {
		t.Run(outcome, func(t *testing.T) {
			a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
			ctx, cfg := context.Background(), codexSchedulerConfig()
			cfg.Protection.MaxAccountAttempts, cfg.Protection.MaxPoolRounds = 1, 1
			req := codexRequest(7, cfg)
			r := codexStart(t, a, req)
			require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
				Reservation: r, Outcome: "upstream_error", RetryScope: "account", RetryAt: time.Now().Add(time.Hour),
			}))
			req.Manual = true
			r = codexStart(t, a, req)
			codexReject(t, a, r)
			r = codexStart(t, a, req)
			require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
			codexFinish(t, a, r, outcome)
			require.Zero(t, r.Status.AttemptsUsed)
			require.Equal(t, 1, r.Status.Round)
			require.Nil(t, r.Status.CooldownUntil)
			require.Nil(t, r.Status.RetryAt)
			req.Manual = false
			next := codexStart(t, a, req)
			codexReject(t, a, next)
			require.Equal(t, 1, next.Status.AttemptsUsed)
			require.Equal(t, "rejection_retry", next.Status.Reason)
		})
	}
}

func TestCodexSchedulerManualBypassesSilenceButPreservesLeaseAndCapacity(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	seed := codexStart(t, a, codexRequest(1, cfg))
	codexReject(t, a, seed)
	manual := codexRequest(2, cfg)
	manual.Manual = true
	r := codexStart(t, a, manual)
	require.True(t, r.HalfOpen)
	other := codexRequest(3, cfg)
	other.Manual = true
	_, err := a.ReserveCodexTicket(ctx, other)
	requireCodexWait(t, err, "half_open_busy")
	codexFinish(t, a, r, "canceled")
	a.settings = proxyPoolSettingsStub{limit: 1}
	a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		return []proxyPoolCandidate{codexPoolCandidate(1, "99")}, nil
	}
	_, err = a.ReserveCodexTicket(ctx, other)
	requireCodexWait(t, err, "capacity")
}

func TestCodexSchedulerRejectionUsesConfiguredLimitsAndReadOnlyStatus(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	server.SetTime(time.Date(2026, time.September, 22, 0, 0, 0, 0, time.UTC))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	cfg.Protection.RejectionRetryIntervalSeconds = 4
	cfg.Protection.RejectionRetryMaxAttempts = 2
	cfg.Protection.RejectionRetryCooldownSeconds = 9
	req := codexRequest(7, cfg)
	for _, delay := range []time.Duration{4 * time.Second, 9 * time.Second} {
		r := codexStart(t, a, req)
		now, err := a.rdb.Time(ctx).Result()
		require.NoError(t, err)
		codexReject(t, a, r)
		require.Equal(t, now.Add(delay).UnixMilli(), r.Status.RetryAt.UnixMilli())
		codexAdvance(t, a, server, delay)
	}
	before := a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val()
	status, err := a.GetCodexTicketRuntimeStatus(ctx, 7)
	require.NoError(t, err)
	require.Zero(t, status.AttemptsUsed)
	require.Equal(t, before, a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val())
	next := codexStart(t, a, req)
	codexReject(t, a, next)
	require.Equal(t, "rejection_retry", next.Status.Reason)
}

func TestCodexSchedulerRejectionRetryWithProtectionDisabled(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	cfg.Protection.Enabled = false
	req := codexRequest(7, cfg)
	for i := 1; i <= 6; i++ {
		r := codexStart(t, a, req)
		codexFinish(t, a, r, "ticket_rejected")
		want, delay := "rejection_retry", 30*time.Second
		if i == 6 {
			want, delay = "rejection_cooldown", 300*time.Second
		}
		_, err := a.ReserveCodexTicket(ctx, req)
		requireCodexWait(t, err, want)
		codexAdvance(t, a, server, delay)
	}
	next := codexStart(t, a, req)
	codexFinish(t, a, next, "ticket_rejected")
	require.Equal(t, "rejection_retry", next.Status.Reason)
}

func TestCodexSchedulerForcedRetryFallsBackWhenAlternativeUnavailable(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "rejection", true: "manual"}[manual], func(t *testing.T) {
			a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
			ctx, cfg := context.Background(), codexSchedulerConfig()
			cfg.ProxyFailureThreshold = 1
			cfg.Protection.ProxySilenceSeconds = 300
			req := codexRequest(7, cfg)
			req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
			r := codexStart(t, a, req)
			codexReject(t, a, r)
			req.Selection.Restricted, req.Selection.IDs = false, nil
			require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 2, &service.ProxyLatencyInfo{Success: false, UpdatedAt: time.Now()}))
			req.Manual = manual
			if !manual {
				codexAdvance(t, a, server, 30*time.Second)
			}
			next := codexStart(t, a, req)
			require.EqualValues(t, 1, next.Proxy.ID)
			codexFinish(t, a, next, "canceled")
		})
	}
}

func TestCodexSchedulerOrdinaryFailureEndsRejectionSequenceWithProtectionDisabled(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	cfg := codexSchedulerConfig()
	cfg.Protection.Enabled = false
	cfg.Protection.RejectionRetryMaxAttempts = 2
	req := codexRequest(7, cfg)
	r := codexStart(t, a, req)
	codexFinish(t, a, r, "ticket_rejected")
	req.Manual = true
	r = codexStart(t, a, req)
	codexFinish(t, a, r, "upstream_error")
	req.Manual = false
	r = codexStart(t, a, req)
	codexFinish(t, a, r, "ticket_rejected")
	require.Equal(t, "rejection_retry", r.Status.Reason, "ordinary failure breaks the consecutive rejection sequence")
}
