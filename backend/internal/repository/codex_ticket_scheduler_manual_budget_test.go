package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexSchedulerManualBypassesCooldownButWaitsForAccountBusy(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx := context.Background()
	cfg := codexSchedulerConfig()
	protection := cfg.TicketProtection()
	protection.MaxAccountAttempts = 1
	cfg.Protection = &protection
	req := codexRequest(7, cfg)
	req.PoolMode, req.FixedProxy = false, codexPoolCandidate(1).proxy

	first := codexStart(t, a, req)
	manual := req
	manual.Manual = true
	_, err := a.ReserveCodexTicket(ctx, manual)
	busy := requireCodexWait(t, err, "account_busy")
	require.Nil(t, busy.CooldownUntil)
	require.Equal(t, 1, busy.AttemptsUsed)
	codexFinish(t, a, first, "upstream_error")
	_, err = a.ReserveCodexTicket(ctx, req)
	requireCodexWait(t, err, "account_cooldown")

	second, err := a.ReserveCodexTicket(ctx, manual)
	require.NoError(t, err, "manual retry must run once during account cooldown")
	require.True(t, second.Manual)
	require.NoError(t, a.StartCodexTicket(ctx, second))
	require.NoError(t, a.CheckCodexTicketStage(ctx, second, second.Proxy))
	require.NoError(t, a.ValidateCodexTicket(ctx, second))
	codexFinish(t, a, second, "upstream_error")
	require.Equal(t, 2, second.Status.AttemptsUsed)
}

func TestCodexSchedulerBudgetIncreaseResumesExhaustedCycle(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx := context.Background()
	cfg := codexSchedulerConfig()
	settings := &codexSchedulerSettingsStub{cfg: cfg}
	a.settings = settings
	req := codexRequest(7, cfg)
	req.PoolMode, req.FixedProxy = false, codexPoolCandidate(1).proxy
	var first *service.CodexTicketReservation
	for range 6 {
		first = codexStart(t, a, req)
		codexFinish(t, a, first, "upstream_error")
		codexAdvance(t, a, server, 2*time.Second)
	}
	_, err := a.ReserveCodexTicket(ctx, req)
	beforeStatus := requireCodexWait(t, err, "account_cooldown")
	require.Equal(t, 6, beforeStatus.AttemptsUsed)
	require.Equal(t, 6, beforeStatus.MaxAttempts)

	updated := cfg
	updatedProtection := cfg.TicketProtection()
	updatedProtection.MaxAccountAttempts = 20
	updated.Protection = &updatedProtection
	settings.cfg = updated
	before := a.rdb.Get(ctx, codexSchedulerAccountKey(req.AccountID)).Val()
	status, err := a.GetCodexTicketRuntimeStatus(ctx, req.AccountID)
	require.NoError(t, err)
	require.Equal(t, before, a.rdb.Get(ctx, codexSchedulerAccountKey(req.AccountID)).Val(), "status must remain read-only")
	require.Equal(t, 6, status.AttemptsUsed)
	require.Equal(t, 20, status.MaxAttempts)
	require.Nil(t, status.CooldownUntil)
	req.Config = updated
	next := codexStart(t, a, req)
	require.Equal(t, first.Generation, next.Generation)
	codexFinish(t, a, next, "upstream_error")
	require.Equal(t, 7, next.Status.AttemptsUsed)
	require.Equal(t, 20, next.Status.MaxAttempts)
}

func TestCodexSchedulerBudgetIncreasePreservesRoundCooldown(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	cfg.Protection.MaxAccountAttempts, cfg.Protection.MaxPoolRounds = 1, 1
	settings := &codexSchedulerSettingsStub{cfg: cfg}
	a.settings = settings
	req := codexRequest(7, cfg)
	r := codexStart(t, a, req)
	codexFinish(t, a, r, "upstream_error")
	changed := cfg
	p := cfg.TicketProtection()
	p.MaxAccountAttempts = 20
	changed.Protection = &p
	settings.cfg, req.Config = changed, changed
	_, err := a.ReserveCodexTicket(ctx, req)
	status := requireCodexWait(t, err, "account_cooldown")
	require.Equal(t, 20, status.MaxAttempts)
}

func TestCodexSchedulerNeutralCompletionDoesNotConsumeFailureBudget(t *testing.T) {
	for _, outcome := range []string{"canceled", "cancelled", "controls_changed", "verification_deferred"} {
		t.Run(outcome, func(t *testing.T) {
			a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
			cfg := codexSchedulerConfig()
			cfg.Protection.MaxAccountAttempts = 2
			req := codexRequest(7, cfg)
			req.PoolMode, req.FixedProxy = false, codexPoolCandidate(1).proxy
			first := codexStart(t, a, req)
			codexFinish(t, a, first, "upstream_error")
			req.Manual = true
			neutral := codexStart(t, a, req)
			codexFinish(t, a, neutral, outcome)
			require.Equal(t, 1, neutral.Status.AttemptsUsed)
			require.Nil(t, neutral.Status.CooldownUntil)
			codexAdvance(t, a, server, 2*time.Second)
			req.Manual = false
			next := codexStart(t, a, req)
			codexFinish(t, a, next, "upstream_error")
			require.Equal(t, 2, next.Status.AttemptsUsed)
			require.NotNil(t, next.Status.CooldownUntil)
		})
	}
}

func TestCodexSchedulerUnstartedCompletionDoesNotRefundPreviousFailure(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	req := codexRequest(7, codexSchedulerConfig())
	req.Manual = true
	first := codexStart(t, a, req)
	codexFinish(t, a, first, "upstream_error")
	next, err := a.ReserveCodexTicket(context.Background(), req)
	require.NoError(t, err)
	codexFinish(t, a, next, "canceled")
	require.Equal(t, 1, next.Status.AttemptsUsed)
}
