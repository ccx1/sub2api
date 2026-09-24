package repository

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexSchedulerIPInflightFailuresCannotSpendMoreThanOneRound(t *testing.T) {
	f := newCodexIPTest(t)
	reservations := make([]*service.CodexTicketReservation, 4)
	for i := range reservations {
		reservations[i] = codexStart(t, f.a, f.request(int64(i+1), 1))
	}
	for _, reservation := range reservations {
		f.finish(t, service.CodexTicketFinishRequest{Reservation: reservation, Outcome: "upstream_error", HarvestProxyFailed: true})
	}
	f.blocked(t, f.request(5, 1), "ip_cooling")
	codexAdvance(t, f.a, f.server, 5*time.Second)
	f.fail(t, f.request(5, 1))
	f.available(t, f.request(6, 1))
	f.fail(t, f.request(6, 1))
	f.blocked(t, f.request(7, 1), "ip_disabled")
}

func TestCodexSchedulerIPPreviousRoundCompletionCannotCountInNextRound(t *testing.T) {
	f := newCodexIPTest(t)
	old := codexStart(t, f.a, f.request(3, 1))
	f.fail(t, f.request(1, 1))
	f.fail(t, f.request(2, 1))
	codexAdvance(t, f.a, f.server, 5*time.Second)
	f.finish(t, service.CodexTicketFinishRequest{Reservation: old, Outcome: "upstream_error", HarvestProxyFailed: true})
	f.fail(t, f.request(4, 1))
	f.available(t, f.request(5, 1))
	f.fail(t, f.request(5, 1))
	f.blocked(t, f.request(6, 1), "ip_disabled")
}

func TestCodexSchedulerIPLateSuccessCannotUndoCooldownOrPermanentDisable(t *testing.T) {
	for _, maxRounds := range []int{1, 2} {
		t.Run(map[int]string{1: "disabled", 2: "cooling"}[maxRounds], func(t *testing.T) {
			f := newCodexIPTest(t)
			f.cfg.Protection.ProxyIPMaxRounds = maxRounds
			old := codexStart(t, f.a, f.request(3, 1))
			f.fail(t, f.request(1, 1))
			f.fail(t, f.request(2, 1))
			f.finish(t, service.CodexTicketFinishRequest{Reservation: old, Outcome: "success", HarvestAccepted: true})
			f.blocked(t, f.request(4, 1), map[int]string{1: "ip_disabled", 2: "ip_cooling"}[maxRounds])
		})
	}
}

func TestCodexSchedulerIPConcurrentAndDuplicateFinishesCountOnce(t *testing.T) {
	f := newCodexIPTest(t)
	f.cfg.Protection.ProxyIPFailureAccountThreshold = 8
	requests := make([]service.CodexTicketFinishRequest, 8)
	for i := range requests {
		requests[i] = service.CodexTicketFinishRequest{
			Reservation: codexStart(t, f.a, f.request(int64(i+1), 1)), Outcome: "upstream_error", HarvestProxyFailed: true,
		}
	}
	var workers sync.WaitGroup
	failures := make(chan error, 32)
	for _, request := range requests {
		for range 4 {
			copyRequest, copyReservation := request, *request.Reservation
			copyRequest.Reservation = &copyReservation
			workers.Go(func() { failures <- f.a.FinishCodexTicket(context.Background(), copyRequest) })
		}
	}
	workers.Wait()
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	f.blocked(t, f.request(9, 1), "ip_cooling")
	codexAdvance(t, f.a, f.server, 5*time.Second)
	for id := int64(9); id <= 15; id++ {
		f.fail(t, f.request(id, 1))
	}
	f.available(t, f.request(16, 1))
	f.fail(t, f.request(16, 1))
	f.blocked(t, f.request(17, 1), "ip_disabled")
}

func TestCodexSchedulerIPProtectionSwitchControlsCountingButPreservesDisable(t *testing.T) {
	f := newCodexIPTest(t)
	f.cfg.Protection.ProxyIPProtectionEnabled = false
	for id := int64(1); id <= 4; id++ {
		f.fail(t, f.request(id, 1))
	}
	f.cfg.Protection.ProxyIPProtectionEnabled = true
	f.fail(t, f.request(5, 1))
	f.available(t, f.request(6, 1))
	f.fail(t, f.request(6, 1))
	f.blocked(t, f.request(7, 1), "ip_cooling")
	codexAdvance(t, f.a, f.server, 5*time.Second)
	f.fail(t, f.request(7, 1))
	f.fail(t, f.request(8, 1))
	f.cfg.Protection.ProxyIPProtectionEnabled = false
	f.blocked(t, f.request(9, 1), "ip_disabled")
}

func TestCodexSchedulerIPReservationCannotStartAfterAnotherAccountCoolsIP(t *testing.T) {
	f := newCodexIPTest(t)
	reserved, err := f.a.ReserveCodexTicket(context.Background(), f.request(3, 1))
	require.NoError(t, err)
	f.fail(t, f.request(1, 1))
	f.fail(t, f.request(2, 1))
	requireCodexWait(t, f.a.StartCodexTicket(context.Background(), reserved), "ip_cooling")
	codexFinish(t, f.a, reserved, "canceled")
}

func TestCodexSchedulerIPBlocksLegacySilenceAndTransportPoolRecovery(t *testing.T) {
	for _, maxRounds := range []int{1, 2} {
		t.Run(map[int]string{1: "disabled", 2: "cooling"}[maxRounds], func(t *testing.T) {
			f := newCodexIPTest(t)
			f.cfg.Protection.Enabled = true
			f.cfg.Protection.ProxyIPMaxRounds = maxRounds
			ctx := context.Background()
			for _, id := range []int64{1, 2} {
				r := codexStart(t, f.a, f.request(id, 1))
				require.NoError(t, f.a.ReportCodexTicketHarvest(ctx, r, false, true, false))
				f.finish(t, service.CodexTicketFinishRequest{
					Reservation: r, Outcome: "ticket_rejected", Silence: true, HarvestProxyFailed: true,
				})
			}
			require.NoError(t, f.a.ReportTransportFailure(ctx, 0, codexIPCandidate(1, "203.0.113.10").proxy))
			for _, manual := range []bool{false, true} {
				req := f.request(3, 0)
				req.Manual = manual
				f.blocked(t, req, map[int]string{1: "ip_disabled", 2: "ip_cooling"}[maxRounds])
			}
		})
	}
}
