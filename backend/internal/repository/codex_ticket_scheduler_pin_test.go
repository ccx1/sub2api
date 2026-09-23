package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newCodexPinTest(t *testing.T, enabled bool) (*ProxyPoolAllocator, *miniredis.Miniredis, service.CodexTicketReserveRequest) {
	t.Helper()
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2), codexPoolCandidate(3))
	cfg := codexSchedulerConfig()
	cfg.Protection.Enabled = enabled
	req := codexRequest(7, cfg)
	req.Manual = true
	return a, server, req
}

func codexSeedPins(t *testing.T, a *ProxyPoolAllocator, req service.CodexTicketReserveRequest) {
	t.Helper()
	ctx := context.Background()
	req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
	r := codexStart(t, a, req)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	b, err := a.SelectCodexTicketBusinessProxy(ctx, service.ProxyPoolSelection{AccountID: req.AccountID, Restricted: true, IDs: []int64{2}}, r)
	require.NoError(t, err)
	require.EqualValues(t, 2, b.ID)
	require.NoError(t, a.CheckCodexTicketStage(ctx, r, b))
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
		Reservation: r, HarvestAccepted: true, BusinessProxySucceeded: true, Outcome: "success", TargetsComplete: true,
	}))
}

func codexPinBusiness(t *testing.T, a *ProxyPoolAllocator, r *service.CodexTicketReservation) *service.Proxy {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	b, err := a.SelectCodexTicketBusinessProxy(ctx, service.ProxyPoolSelection{AccountID: r.AccountID}, r)
	require.NoError(t, err)
	require.NotNil(t, b)
	require.NoError(t, a.CheckCodexTicketStage(ctx, r, b))
	return b
}

func TestCodexSchedulerPinStickyAcrossModelsInstancesAndProtection(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(strconv.FormatBool(enabled), func(t *testing.T) {
			a, server, req := newCodexPinTest(t, enabled)
			codexSeedPins(t, a, req)
			other := *a
			other.rdb = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
			t.Cleanup(func() { _ = other.rdb.Close() })
			req.Model, req.Models = "sol", []string{"sol"}
			r := codexStart(t, &other, req)
			require.EqualValues(t, 1, r.Proxy.ID)
			require.EqualValues(t, 2, codexPinBusiness(t, &other, r).ID)
			codexFinish(t, &other, r, "success")
			ctx := context.Background()
			require.Equal(t, "2", a.rdb.HGet(ctx, proxyPoolAffinityKey("7"), "proxy_id").Val())
			require.NoError(t, a.rdb.ZScore(ctx, proxyPoolLeaseKey("2"), "7").Err())
		})
	}
}

func TestCodexSchedulerPinFirstCandidateStickyBeforeSuccess(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	ctx := context.Background()
	req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
	r := codexStart(t, a, req)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	require.NoError(t, a.CheckCodexTicketStage(ctx, r, codexPoolCandidate(2).proxy))
	codexFinish(t, a, r, "verification_failed")
	req.Selection.Restricted, req.Selection.IDs = false, nil
	next := codexStart(t, a, req)
	require.EqualValues(t, 1, next.Proxy.ID, "the first real harvest confirms A before the first successful ticket")
	require.EqualValues(t, 2, codexPinBusiness(t, a, next).ID, "the first real verification confirms B")
	codexFinish(t, a, next, "canceled")
}

func TestCodexSchedulerPinIndependentFailuresAndHarvestReset(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	req.Config.ProxyFailureThreshold = 2
	codexSeedPins(t, a, req)
	ctx := context.Background()
	for range 2 {
		r := codexStart(t, a, req)
		require.EqualValues(t, 1, r.Proxy.ID)
		require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
			Reservation: r, HarvestProxyFailed: true, Outcome: "upstream_error",
		}))
		r = codexStart(t, a, req)
		require.EqualValues(t, 1, r.Proxy.ID)
		require.EqualValues(t, 2, codexPinBusiness(t, a, r).ID)
		require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
			Reservation: r, HarvestAccepted: true, BusinessProxyFailed: true, Outcome: "verification_failed",
		}))
	}
	r := codexStart(t, a, req)
	require.EqualValues(t, 1, r.Proxy.ID, "accepted harvest resets only A failures")
	require.NotEqualValues(t, 2, codexPinBusiness(t, a, r).ID, "B failures accumulate independently")
	codexFinish(t, a, r, "success")
}

func TestCodexSchedulerPinBusinessSuccessResetsBeforePublish(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	req.Config.ProxyFailureThreshold = 2
	codexSeedPins(t, a, req)
	ctx := context.Background()
	for _, successful := range []bool{false, true, false} {
		r := codexStart(t, a, req)
		require.EqualValues(t, 2, codexPinBusiness(t, a, r).ID)
		outcome := "verification_failed"
		if successful {
			outcome = "canceled"
		}
		require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
			Reservation: r, HarvestAccepted: true, BusinessProxyFailed: !successful,
			BusinessProxySucceeded: successful, Outcome: outcome,
		}))
	}
	r := codexStart(t, a, req)
	require.EqualValues(t, 2, codexPinBusiness(t, a, r).ID)
	codexFinish(t, a, r, "success")
}

func TestCodexSchedulerPinThresholdSwitchesDifferentIDAndFencesFinish(t *testing.T) {
	for _, single := range []bool{false, true} {
		t.Run(strconv.FormatBool(single), func(t *testing.T) {
			a, _, req := newCodexPinTest(t, false)
			codexSeedPins(t, a, req)
			if single {
				req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
			}
			ctx := context.Background()
			var stale service.CodexTicketFinishRequest
			for i := 0; i < 3; i++ {
				r := codexStart(t, a, req)
				require.EqualValues(t, 1, r.Proxy.ID)
				if i > 0 {
					before := a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val()
					require.NoError(t, a.FinishCodexTicket(ctx, stale))
					require.Equal(t, before, a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val())
				}
				stale = service.CodexTicketFinishRequest{Reservation: r, HarvestProxyFailed: true, Outcome: "upstream_error"}
				require.NoError(t, a.FinishCodexTicket(ctx, stale))
				require.NoError(t, a.FinishCodexTicket(ctx, stale))
			}
			req.Manual = false
			r, err := a.ReserveCodexTicket(ctx, req)
			if single {
				requireCodexWait(t, err, "proxy_switch_waiting")
				return
			}
			require.NoError(t, err)
			require.NotEqualValues(t, 1, r.Proxy.ID)
			codexFinish(t, a, r, "canceled")
		})
	}
}

func TestCodexSchedulerPinCanceledReservationDoesNotConfirmHarvest(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	ctx := context.Background()
	req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
	r, err := a.ReserveCodexTicket(ctx, req)
	require.NoError(t, err)
	codexFinish(t, a, r, "canceled")
	require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 1, &service.ProxyLatencyInfo{Success: false, UpdatedAt: time.Now()}))
	req.Selection.Restricted, req.Selection.IDs = false, nil
	r = codexStart(t, a, req)
	require.NotEqualValues(t, 1, r.Proxy.ID)
	codexFinish(t, a, r, "canceled")
}

func TestCodexSchedulerPinSelectedBusinessWithoutStageIsNotConfirmed(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	ctx := context.Background()
	req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
	r := codexStart(t, a, req)
	_, err := a.SelectCodexTicketBusinessProxy(ctx, service.ProxyPoolSelection{AccountID: 7, Restricted: true, IDs: []int64{2}}, r)
	require.NoError(t, err)
	codexFinish(t, a, r, "canceled")
	require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 2, &service.ProxyLatencyInfo{Success: false, UpdatedAt: time.Now()}))
	r = codexStart(t, a, req)
	require.NotEqualValues(t, 2, codexPinBusiness(t, a, r).ID)
	codexFinish(t, a, r, "canceled")
}

func TestCodexSchedulerPinTemporaryUnavailabilityWaits(t *testing.T) {
	for _, reason := range []string{"proxy_unhealthy", "capacity", "proxy_silent"} {
		t.Run(reason, func(t *testing.T) {
			a, server, req := newCodexPinTest(t, true)
			codexSeedPins(t, a, req)
			req.Manual = false
			ctx := context.Background()
			switch reason {
			case "proxy_unhealthy":
				require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 1, &service.ProxyLatencyInfo{Success: false, UpdatedAt: time.Now()}))
			case "capacity":
				a.settings = proxyPoolSettingsStub{limit: 1}
				a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
					return []proxyPoolCandidate{codexPoolCandidate(1, "8"), codexPoolCandidate(2), codexPoolCandidate(3)}, nil
				}
			case "proxy_silent":
				other := codexRequest(8, req.Config)
				other.Selection.Restricted, other.Selection.IDs = true, []int64{1}
				r := codexStart(t, a, other)
				require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, false, true, false))
				codexFinish(t, a, r, "ticket_rejected")
			}
			reserved, err := a.ReserveCodexTicket(ctx, req)
			if reason == "proxy_silent" {
				require.NoError(t, err)
				require.NotEqualValues(t, 1, reserved.Proxy.ID, "an available proxy takes precedence over a silent pin")
				require.False(t, reserved.HalfOpen)
				codexFinish(t, a, reserved, "canceled")
				return
			}
			requireCodexWait(t, err, reason)
			a.settings = proxyPoolSettingsStub{limit: 0}
			a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
				return []proxyPoolCandidate{codexPoolCandidate(1), codexPoolCandidate(2), codexPoolCandidate(3)}, nil
			}
			require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 1, &service.ProxyLatencyInfo{Success: true, UpdatedAt: time.Now()}))
			codexAdvance(t, a, server, 6*time.Second)
			r := codexStart(t, a, req)
			require.EqualValues(t, 1, r.Proxy.ID)
			codexFinish(t, a, r, "canceled")
		})
	}
}

func TestCodexSchedulerPinInvalidationReleasesHarvestRoute(t *testing.T) {
	for _, change := range []string{"removed", "disabled", "version"} {
		t.Run(change, func(t *testing.T) {
			a, _, req := newCodexPinTest(t, false)
			codexSeedPins(t, a, req)
			candidates := []proxyPoolCandidate{codexPoolCandidate(1), codexPoolCandidate(2), codexPoolCandidate(3)}
			switch change {
			case "removed":
				candidates = candidates[1:]
			case "disabled":
				candidates[0].proxy.Status = "disabled"
			case "version":
				candidates[0].proxy.Port++
			}
			a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
				return candidates, nil
			}
			r := codexStart(t, a, req)
			if change != "version" {
				require.NotEqualValues(t, 1, r.Proxy.ID)
			} else if r.Proxy.ID == 1 {
				require.Equal(t, 8081, r.Proxy.Port)
			}
			codexFinish(t, a, r, "canceled")
		})
	}
}
