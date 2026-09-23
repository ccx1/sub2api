package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type codexSchedulerSettingsStub struct {
	cfg           config.OpenAICodexTicketConfig
	limit         int
	disabledAt    time.Time
	transitionErr error
}

func (s *codexSchedulerSettingsStub) GetProxyPoolMaxAccounts(context.Context) (int, error) {
	return s.limit, nil
}
func (s *codexSchedulerSettingsStub) GetCodexTicketSettings(context.Context) (config.OpenAICodexTicketConfig, error) {
	return s.cfg, nil
}

func (s *codexSchedulerSettingsStub) GetCodexTicketProtectionDisabledAt(context.Context) (time.Time, error) {
	return s.disabledAt, s.transitionErr
}

func TestCodexSchedulerNeutralHalfOpenAndStaleOwnerCannotRecover(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	fixedRequest := func(id int64) service.CodexTicketReserveRequest {
		req := codexRequest(id, cfg)
		req.PoolMode, req.FixedProxy = false, codexPoolCandidate(1).proxy
		return req
	}
	r := codexStart(t, a, fixedRequest(1))
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, false, true, false))
	codexFinish(t, a, r, "ticket_rejected")
	codexAdvance(t, a, server, 6*time.Second)
	old := codexStart(t, a, fixedRequest(2))
	codexAdvance(t, a, server, 33*time.Second)
	current := codexStart(t, a, fixedRequest(3))
	require.True(t, current.HalfOpen)
	require.Error(t, a.ReportCodexTicketHarvest(ctx, old, true, false, false))
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: old, Outcome: "success"}))
	_, err := a.ReserveCodexTicket(ctx, fixedRequest(4))
	requireCodexWait(t, err, "half_open_busy")
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, current, false, false, true))
	codexFinish(t, a, current, "canceled")
	_, err = a.ReserveCodexTicket(ctx, fixedRequest(4))
	requireCodexWait(t, err, "proxy_silent")
	codexAdvance(t, a, server, 2*time.Second)
	next := codexStart(t, a, fixedRequest(4))
	require.True(t, next.HalfOpen)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, next, false, false, false))
	codexFinish(t, a, next, "model_mismatch")
	_, err = a.ReserveCodexTicket(ctx, fixedRequest(5))
	requireCodexWait(t, err, "proxy_silent")
}

func TestCodexSchedulerDeferredBusinessDoesNotPunishHarvestOrBypassRecovery(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	breq := codexRequest(2, cfg)
	breq.PoolMode, breq.FixedProxy = false, codexPoolCandidate(2).proxy
	b := codexStart(t, a, breq)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, b, false, true, false))
	codexFinish(t, a, b, "ticket_rejected")
	areq := codexRequest(1, cfg)
	areq.PoolMode, areq.FixedProxy = false, codexPoolCandidate(1).proxy
	ar := codexStart(t, a, areq)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, ar, true, false, false))
	requireCodexWait(t, a.CheckCodexTicketStage(ctx, ar, codexPoolCandidate(2).proxy), "proxy_silent")
	codexFinish(t, a, ar, "verification_deferred")
	_, err := a.ReserveCodexTicket(ctx, areq)
	requireCodexWait(t, err, "verification_deferred")
	codexAdvance(t, a, server, 6*time.Second)
	_, err = a.ReserveCodexTicket(ctx, areq)
	requireCodexWait(t, err, "verification_deferred")
	breq.AccountID, breq.Selection.AccountID = 3, 3
	b = codexStart(t, a, breq)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, b, true, false, false))
	codexFinish(t, a, b, "success")
	ar = codexStart(t, a, areq)
	require.NoError(t, a.CheckCodexTicketStage(ctx, ar, codexPoolCandidate(2).proxy))
	codexFinish(t, a, ar, "success")
	status, err := a.GetCodexTicketRuntimeStatus(ctx, 1)
	require.NoError(t, err)
	require.Zero(t, status.AttemptsUsed)
}

func TestCodexSchedulerSharedBackoffManualBypassesAccountRetry(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	req := codexRequest(7, cfg)
	req.PoolMode, req.FixedProxy = false, codexPoolCandidate(1).proxy
	r := codexStart(t, a, req)
	codexFinish(t, a, r, "upstream_error")
	_, err := a.ReserveCodexTicket(ctx, req)
	requireCodexWait(t, err, "model_backoff")
	req.Manual = true
	r = codexStart(t, a, req)
	now, err := a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: r, Outcome: "upstream_error", RetryScope: "account", RetryAt: now.Add(8 * time.Second)}))
	req.Model, req.Models = "sol", []string{"sol"}
	r, err = a.ReserveCodexTicket(ctx, req)
	require.NoError(t, err)
	codexFinish(t, a, r, "canceled")
	_ = server
}

func TestCodexSchedulerProtectionOffDrainsWithoutResettingBudget(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	settings := &codexSchedulerSettingsStub{cfg: cfg}
	a.settings = settings
	req := codexRequest(7, cfg)
	r := codexStart(t, a, req)
	codexFinish(t, a, r, "upstream_error")
	off := cfg
	protection := cfg.TicketProtection()
	protection.Enabled = false
	off.Protection = &protection
	settings.cfg, req.Config = off, off
	_, err := a.ReserveCodexTicket(ctx, req)
	status := requireCodexWait(t, err, "protection_draining")
	require.Equal(t, 1, status.AttemptsUsed)
	settings.cfg, req.Config = cfg, cfg
	_, err = a.ReserveCodexTicket(ctx, req)
	requireCodexWait(t, err, "protection_draining")
	codexAdvance(t, a, server, 11*time.Second)
	next := codexStart(t, a, req)
	require.Greater(t, next.Generation, r.Generation)
	codexFinish(t, a, next, "success")
}

func TestCodexSchedulerPolicyChangedBeforeSendHasNoCharge(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	settings := &codexSchedulerSettingsStub{cfg: cfg}
	a.settings = settings
	r, err := a.ReserveCodexTicket(ctx, codexRequest(7, cfg))
	require.NoError(t, err)
	changed := cfg
	changed.HarvestAttemptTimeoutSeconds++
	settings.cfg = changed
	requireCodexWait(t, a.StartCodexTicket(ctx, r), "controls_changed")
	codexFinish(t, a, r, "controls_changed")
	status, err := a.GetCodexTicketRuntimeStatus(ctx, 7)
	require.NoError(t, err)
	require.Zero(t, status.AttemptsUsed)
}

func TestCodexSchedulerSingleAccountLeaseAndAtomicCapacityAcrossInstances(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 2, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	other := *a
	var wg sync.WaitGroup
	var mu sync.Mutex
	reservations := []*service.CodexTicketReservation{}
	for i := int64(1); i <= 20; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			r, err := other.ReserveCodexTicket(ctx, codexRequest(id, cfg))
			if err == nil {
				mu.Lock()
				reservations = append(reservations, r)
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	require.Len(t, reservations, 2)
	for _, r := range reservations {
		req := codexRequest(r.AccountID, cfg)
		req.Model, req.Models = "sol", []string{"sol"}
		_, err := a.ReserveCodexTicket(ctx, req)
		requireCodexWait(t, err, "account_busy")
		codexFinish(t, a, r, "canceled")
	}
	require.Zero(t, a.rdb.ZCard(ctx, "proxy:{pool}:codex:leases:1").Val())
}
