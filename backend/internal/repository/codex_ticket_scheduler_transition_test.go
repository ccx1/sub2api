package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexSchedulerUnobservedDisableUsesPersistedDeadline(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	settings := &codexSchedulerSettingsStub{cfg: cfg}
	a.settings = settings
	req := codexRequest(7, cfg)
	req.Manual = true
	r := codexStart(t, a, req)
	codexFinish(t, a, r, "upstream_error")
	codexAdvance(t, a, server, time.Second)
	disabled, err := a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	settings.disabledAt = disabled
	// 管理员已经重新开启；调度只看到最新开启设置与持久化关闭水位。
	codexAdvance(t, a, server, 3*time.Second)
	req.Manual = false
	_, err = a.ReserveCodexTicket(ctx, req)
	status := requireCodexWait(t, err, "protection_draining")
	require.Equal(t, 1, status.AttemptsUsed)
	require.WithinDuration(t, disabled.Add(10*time.Second), *status.CooldownUntil, time.Millisecond)
	codexAdvance(t, a, server, 8*time.Second)
	next := codexStart(t, a, req)
	require.Greater(t, next.Generation, r.Generation)
	codexFinish(t, a, next, "success")
	// 老关闭水位不得重新排空已经创建的新周期。
	next = codexStart(t, a, req)
	codexFinish(t, a, next, "success")
}

func TestCodexSchedulerProtectionTransitionReadFailureIsClosed(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	settings := &codexSchedulerSettingsStub{cfg: cfg}
	a.settings = settings
	r, err := a.ReserveCodexTicket(ctx, codexRequest(7, cfg))
	require.NoError(t, err)
	before := a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val()
	settings.transitionErr = errors.New("settings unavailable")
	require.ErrorContains(t, a.StartCodexTicket(ctx, r), "protection transition")
	require.Equal(t, before, a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val())
	_, err = a.ReserveCodexTicket(ctx, codexRequest(8, cfg))
	require.ErrorContains(t, err, "protection transition")
	require.Zero(t, a.rdb.Exists(ctx, codexSchedulerAccountKey(8)).Val())
}

func TestCodexSchedulerUnobservedDisableStopsInflightStageAndPublish(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	settings := &codexSchedulerSettingsStub{cfg: cfg}
	a.settings = settings
	r := codexStart(t, a, codexRequest(7, cfg))
	// 数据库与 Redis 的毫秒时钟不必严格同步；新水位即代表尚未观察到的关闭。
	now, err := a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	settings.disabledAt = now.Add(-time.Millisecond)
	requireCodexWait(t, a.CheckCodexTicketStage(ctx, r, r.Proxy), "protection_draining")
	requireCodexWait(t, a.ValidateCodexTicket(ctx, r), "protection_draining")
	codexFinish(t, a, r, "controls_changed")
	require.Zero(t, r.Status.AttemptsUsed, "a policy cancellation must refund its in-flight attempt")
}

func TestCodexSchedulerUnobservedDisableFencesHarvestProtectionReport(t *testing.T) {
	for _, halfOpen := range []bool{false, true} {
		name := "rejected_report_does_not_create_silence"
		if halfOpen {
			name = "accepted_report_does_not_recover_half_open"
		}
		t.Run(name, func(t *testing.T) {
			a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
			ctx, cfg := context.Background(), codexSchedulerConfig()
			settings := &codexSchedulerSettingsStub{cfg: cfg}
			a.settings = settings
			if halfOpen {
				seed := codexStart(t, a, codexRequest(1, cfg))
				require.NoError(t, a.ReportCodexTicketHarvest(ctx, seed, false, true, false))
				codexFinish(t, a, seed, "ticket_rejected")
				codexAdvance(t, a, server, 6*time.Second)
			}
			r := codexStart(t, a, codexRequest(2, cfg))
			require.Equal(t, halfOpen, r.HalfOpen)
			disabled, err := a.rdb.Time(ctx).Result()
			require.NoError(t, err)
			settings.disabledAt = disabled
			require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, halfOpen, !halfOpen, false))
			codexFinish(t, a, r, "controls_changed")
			require.False(t, r.Status.RuleMatched)
			next, err := a.ReserveCodexTicket(ctx, codexRequest(3, cfg))
			require.NoError(t, err)
			require.Equal(t, halfOpen, next.HalfOpen, "a stale accepted report must not restore the proxy to available")
			codexFinish(t, a, next, "canceled")
		})
	}
}

func TestCodexSchedulerFinishSnapshotStaysWithItsAttempt(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	req := codexRequest(7, cfg)
	req.Manual = true
	first := codexStart(t, a, req)
	codexFinish(t, a, first, "success")
	require.Zero(t, first.Status.AttemptsUsed)
	require.Equal(t, "success", first.Status.Reason)
	next := codexStart(t, a, req)
	codexFinish(t, a, next, "upstream_error")
	require.Equal(t, 1, next.Status.AttemptsUsed)
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: first, Outcome: "success"}))
	require.Zero(t, first.Status.AttemptsUsed, "a late duplicate must not replace history with a later attempt")
}

func TestCodexSchedulerDeferredWithoutChosenBusinessProxy(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	req := codexRequest(7, cfg)
	req.Manual = true
	r := codexStart(t, a, req)
	now, err := a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
		Reservation: r, Outcome: "verification_deferred", DeferredUntil: now.Add(3 * time.Second),
	}))
	req.Manual = false
	_, err = a.ReserveCodexTicket(ctx, req)
	require.Zero(t, requireCodexWait(t, err, "verification_deferred").AttemptsUsed)
	codexAdvance(t, a, server, 4*time.Second)
	r = codexStart(t, a, req)
	codexFinish(t, a, r, "success")
}

func TestCodexSchedulerProxyResolutionKeepsTemporaryUnavailableDistinct(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		limit      int
		candidates []proxyPoolCandidate
		wantReason string
	}{
		{name: "capacity", limit: 1, candidates: []proxyPoolCandidate{codexPoolCandidate(1, "99")}, wantReason: "capacity"},
		{name: "unhealthy", candidates: []proxyPoolCandidate{{proxy: &service.Proxy{ID: 1, Status: "disabled"}}}, wantReason: "proxy_unhealthy"},
		{name: "empty"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			a, _ := newProxyPoolAllocatorTest(t, scenario.limit, scenario.candidates...)
			ctx := context.Background()
			selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
			require.NoError(t, err)
			require.Nil(t, selected, "ordinary business selection keeps its existing empty policy")
			selected, err = a.Select(service.WithCodexTicketProxyResolution(ctx), service.ProxyPoolSelection{AccountID: 7})
			require.Nil(t, selected)
			if scenario.wantReason == "" {
				require.NoError(t, err)
				return
			}
			require.NotNil(t, requireCodexWait(t, err, scenario.wantReason).RetryAt)
		})
	}
}
