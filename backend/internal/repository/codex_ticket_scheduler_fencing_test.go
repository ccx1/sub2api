package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexSchedulerManualBypassesHardModelDeadline(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	req := codexRequest(7, cfg)
	req.PoolMode, req.FixedProxy = false, codexPoolCandidate(1).proxy
	r := codexStart(t, a, req)
	now, err := a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: r, Outcome: "upstream_error", RetryScope: "model", RetryAt: now.Add(9 * time.Second)}))
	require.WithinDuration(t, now.Add(9*time.Second), *r.Status.RetryAt, time.Millisecond)
	req.Manual, req.Models = true, []string{"astra", "sol"}
	r, err = a.ReserveCodexTicket(ctx, req)
	require.NoError(t, err)
	codexFinish(t, a, r, "success")
	_ = server
}

func TestCodexSchedulerStaleCompletionDoesNotWriteNewReservation(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	req := codexRequest(7, cfg)
	req.PoolMode, req.FixedProxy = false, codexPoolCandidate(1).proxy
	old := codexStart(t, a, req)
	codexAdvance(t, a, server, 33*time.Second)
	current := codexStart(t, a, req)
	before := a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val()
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: old, Outcome: "success", TargetsComplete: true}))
	require.Equal(t, before, a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val())
	require.Error(t, a.ReportCodexTicketHarvest(ctx, old, true, false, false))
	require.Equal(t, before, a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val())
	require.NoError(t, a.ValidateCodexTicket(ctx, current))
	codexFinish(t, a, current, "success")
}

func TestCodexSchedulerPublishRechecksPolicyWithoutProxyGuard(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	settings := &codexSchedulerSettingsStub{cfg: cfg}
	a.settings = settings
	r := codexStart(t, a, codexRequest(7, cfg))
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	other := codexStart(t, a, codexRequest(8, cfg))
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, other, false, true, false))
	codexFinish(t, a, other, "ticket_rejected")
	require.NoError(t, a.ValidateCodexTicket(ctx, r), "new silence must not reject already-completed verification")
	settings.cfg.TTLSeconds++
	requireCodexWait(t, a.ValidateCodexTicket(ctx, r), "controls_changed")
	codexFinish(t, a, r, "controls_changed")
}

func TestCodexSchedulerDeferredCapacityRechecksWithoutNewHarvest(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 1, codexPoolCandidate(1), codexPoolCandidate(2))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	busyReq := codexRequest(8, cfg)
	busyReq.PoolMode, busyReq.FixedProxy = false, codexPoolCandidate(2).proxy
	busy := codexStart(t, a, busyReq)
	req := codexRequest(7, cfg)
	req.PoolMode, req.FixedProxy = false, codexPoolCandidate(1).proxy
	r := codexStart(t, a, req)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	blocked := requireCodexWait(t, a.CheckCodexTicketStage(ctx, r, codexPoolCandidate(2).proxy), "capacity")
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: r, Outcome: "verification_deferred", DeferredProxyID: 2, DeferredUntil: *blocked.RetryAt}))
	_, err := a.ReserveCodexTicket(ctx, req)
	require.Zero(t, requireCodexWait(t, err, "verification_deferred").AttemptsUsed)
	codexAdvance(t, a, server, 2*time.Second)
	_, err = a.ReserveCodexTicket(ctx, req)
	require.Zero(t, requireCodexWait(t, err, "verification_deferred").AttemptsUsed)
	codexFinish(t, a, busy, "success")
	codexAdvance(t, a, server, 2*time.Second)
	r = codexStart(t, a, req)
	require.NoError(t, a.CheckCodexTicketStage(ctx, r, codexPoolCandidate(2).proxy))
	codexFinish(t, a, r, "success")
}

func TestCodexSchedulerBackoffJitterNeverAdvancesHardDeadline(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	cfg.RetryBackoffSeconds = []int{20}
	r := codexStart(t, a, codexRequest(7, cfg))
	before, err := a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	codexFinish(t, a, r, "upstream_error")
	after, err := a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	var state struct {
		Retries map[string]struct {
			Until int64 `json:"until_at"`
		} `json:"retries"`
	}
	require.NoError(t, json.Unmarshal([]byte(a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val()), &state))
	deadline := time.UnixMilli(state.Retries["astra"].Until)
	require.False(t, deadline.Before(before.Add(20*time.Second)))
	require.False(t, deadline.After(after.Add(22*time.Second)))
}
