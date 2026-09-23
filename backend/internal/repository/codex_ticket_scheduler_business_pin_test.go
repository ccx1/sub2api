package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestCodexSchedulerPinBusinessTemporaryUnavailabilityWaits(t *testing.T) {
	for _, reason := range []string{"proxy_unhealthy", "capacity", "proxy_silent"} {
		t.Run(reason, func(t *testing.T) {
			a, server, req := newCodexPinTest(t, true)
			codexSeedPins(t, a, req)
			req.Manual = false
			ctx := context.Background()
			other := codexRequest(8, req.Config)
			other.Selection.Restricted, other.Selection.IDs = true, []int64{2}
			switch reason {
			case "proxy_unhealthy":
				require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 2, &service.ProxyLatencyInfo{Success: false, UpdatedAt: time.Now()}))
			case "capacity":
				a.settings = proxyPoolSettingsStub{limit: 1}
				a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
					return []proxyPoolCandidate{codexPoolCandidate(1), codexPoolCandidate(2, "8"), codexPoolCandidate(3)}, nil
				}
			case "proxy_silent":
				r := codexStart(t, a, other)
				require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, false, true, false))
				codexFinish(t, a, r, "ticket_rejected")
			}
			r := codexStart(t, a, req)
			require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
			_, err := a.SelectCodexTicketBusinessProxy(ctx, req.Selection, r)
			requireCodexWait(t, err, reason)
			codexFinish(t, a, r, "canceled")
			a.settings = proxyPoolSettingsStub{limit: 0}
			a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
				return []proxyPoolCandidate{codexPoolCandidate(1), codexPoolCandidate(2), codexPoolCandidate(3)}, nil
			}
			require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 2, &service.ProxyLatencyInfo{Success: true, UpdatedAt: time.Now()}))
			codexAdvance(t, a, server, 6*time.Second)
			if reason == "proxy_silent" {
				other.Manual = true
				half := codexStart(t, a, other)
				require.NoError(t, a.ReportCodexTicketHarvest(ctx, half, true, false, false))
				codexFinish(t, a, half, "success")
			}
			r = codexStart(t, a, req)
			require.EqualValues(t, 2, codexPinBusiness(t, a, r).ID)
			codexFinish(t, a, r, "success")
		})
	}
}

func TestCodexSchedulerPinBusinessInvalidationUsesCurrentCandidates(t *testing.T) {
	for _, change := range []string{"removed", "version", "region"} {
		t.Run(change, func(t *testing.T) {
			a, _, req := newCodexPinTest(t, false)
			codexSeedPins(t, a, req)
			ctx := context.Background()
			r := codexStart(t, a, req)
			require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
			candidates := []proxyPoolCandidate{codexPoolCandidate(1), codexPoolCandidate(2), codexPoolCandidate(3)}
			selection := service.ProxyPoolSelection{AccountID: req.AccountID}
			switch change {
			case "removed":
				candidates = []proxyPoolCandidate{candidates[0], candidates[2]}
			case "version":
				candidates[1].proxy.Port++
				selection.Restricted, selection.IDs = true, []int64{2}
			case "region":
				selection.CountryCode = "JP"
				for id, country := range map[int64]string{1: "JP", 2: "US", 3: "JP"} {
					require.NoError(t, a.latencyCache.SetProxyLatency(ctx, id, &service.ProxyLatencyInfo{Success: true, CountryCode: country, UpdatedAt: time.Now()}))
				}
			}
			a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
				return candidates, nil
			}
			b, err := a.SelectCodexTicketBusinessProxy(ctx, selection, r)
			require.NoError(t, err)
			require.NotNil(t, b)
			if change == "version" {
				require.EqualValues(t, 2, b.ID)
				require.Equal(t, 8081, b.Port)
			} else {
				require.NotEqualValues(t, 2, b.ID)
			}
			require.NoError(t, a.CheckCodexTicketStage(ctx, r, b))
			codexFinish(t, a, r, "success")
		})
	}
}

func TestCodexSchedulerPinBusinessThresholdWaitPreservesOtherAccounts(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	codexSeedPins(t, a, req)
	ctx := context.Background()
	now, err := a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	require.NoError(t, a.rdb.ZAdd(ctx, proxyPoolLeaseKey("2"), redis.Z{Score: float64(now.Add(time.Hour).Unix()), Member: "8"}).Err())
	require.NoError(t, a.rdb.SAdd(ctx, proxyPoolBoundAccountsKey("2"), "7", "8").Err())
	for _, model := range []string{"astra", "sol", "astra", "astra"} {
		req.Model, req.Models = model, []string{"astra", "sol"}
		r := codexStart(t, a, req)
		require.EqualValues(t, 1, r.Proxy.ID)
		require.EqualValues(t, 2, codexPinBusiness(t, a, r).ID)
		require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
			Reservation: r, HarvestAccepted: true, BusinessProxyFailed: true, Outcome: "verification_failed",
		}))
	}
	before := a.rdb.HGetAll(ctx, proxyPoolAffinityKey("7")).Val()
	require.Empty(t, before, "threshold must invalidate the matching business affinity")
	require.ErrorIs(t, a.rdb.ZScore(ctx, proxyPoolLeaseKey("2"), "7").Err(), redis.Nil)
	require.NoError(t, a.rdb.ZScore(ctx, proxyPoolLeaseKey("2"), "8").Err())
	require.False(t, a.rdb.SIsMember(ctx, proxyPoolBoundAccountsKey("2"), "7").Val())
	require.True(t, a.rdb.SIsMember(ctx, proxyPoolBoundAccountsKey("2"), "8").Val())
	req.Manual, req.Models = false, []string{req.Model}
	r := codexStart(t, a, req)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	_, err = a.SelectCodexTicketBusinessProxy(ctx, service.ProxyPoolSelection{AccountID: 7, Restricted: true, IDs: []int64{2}}, r)
	requireCodexWait(t, err, "proxy_switch_waiting")
	require.Equal(t, before, a.rdb.HGetAll(ctx, proxyPoolAffinityKey("7")).Val())
	require.NoError(t, a.rdb.ZScore(ctx, proxyPoolLeaseKey("2"), "8").Err())
	codexFinish(t, a, r, "canceled")
}

func TestCodexSchedulerPinBusinessThresholdBeforeFirstSuccess(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		r := codexStart(t, a, req)
		require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
		selection := service.ProxyPoolSelection{AccountID: 7}
		if i == 0 {
			selection.Restricted, selection.IDs = true, []int64{2}
		}
		b, err := a.SelectCodexTicketBusinessProxy(ctx, selection, r)
		require.NoError(t, err)
		require.EqualValues(t, 2, b.ID)
		require.NoError(t, a.CheckCodexTicketStage(ctx, r, b))
		require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
			Reservation: r, HarvestAccepted: true, BusinessProxyFailed: true, Outcome: "verification_failed",
		}))
	}
	require.Zero(t, a.rdb.Exists(ctx, proxyPoolAffinityKey("7")).Val())
	r := codexStart(t, a, req)
	require.NotEqualValues(t, 2, codexPinBusiness(t, a, r).ID)
	codexFinish(t, a, r, "success")
}

func TestCodexSchedulerPinHarvestThresholdDoesNotRejectExplicitFixedProxy(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	req.Config.ProxyFailureThreshold = 1
	codexSeedPins(t, a, req)
	r := codexStart(t, a, req)
	require.EqualValues(t, 1, r.Proxy.ID)
	require.NoError(t, a.FinishCodexTicket(context.Background(), service.CodexTicketFinishRequest{
		Reservation: r, HarvestProxyFailed: true, Outcome: "upstream_error",
	}))
	req.PoolMode, req.FixedProxy = false, codexPoolCandidate(1).proxy
	r = codexStart(t, a, req)
	require.EqualValues(t, 1, r.Proxy.ID)
	codexFinish(t, a, r, "canceled")
}

func TestCodexSchedulerPinFollowBusinessSharesNodeAndLogicalFailures(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	req.HarvestUsesBusiness = true
	ctx := context.Background()
	var initial int64
	for i := 0; i < 3; i++ {
		if i == 0 {
			req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
		} else {
			req.Selection.Restricted, req.Selection.IDs = false, nil
		}
		r := codexStart(t, a, req)
		if i == 0 {
			initial = r.Proxy.ID
		}
		require.Equal(t, initial, r.Proxy.ID)
		finish := service.CodexTicketFinishRequest{Reservation: r, HarvestProxyFailed: i == 0, Outcome: "upstream_error"}
		if i > 0 {
			require.Equal(t, r.Proxy.ID, codexPinBusiness(t, a, r).ID)
			finish.HarvestAccepted, finish.BusinessProxyFailed, finish.Outcome = true, true, "verification_failed"
		}
		require.NoError(t, a.FinishCodexTicket(ctx, finish))
		require.NoError(t, a.FinishCodexTicket(ctx, finish))
	}
	r := codexStart(t, a, req)
	require.NotEqual(t, initial, r.Proxy.ID)
	require.Equal(t, r.Proxy.ID, codexPinBusiness(t, a, r).ID)
	codexFinish(t, a, r, "success")
}

func TestCodexSchedulerPinDirectFallbackRequiresTrulyEmptyPool(t *testing.T) {
	for _, mode := range []string{"empty", "region_empty", "region_no_match", "region_no_allow", "unhealthy", "region_unhealthy", "capacity", "silent"} {
		t.Run(mode, func(t *testing.T) {
			a, _, req := newCodexPinTest(t, true)
			req.AllowDirectOnEmpty, req.HarvestUsesBusiness = true, true
			req.Manual = false
			ctx := context.Background()
			candidates := []proxyPoolCandidate{}
			if mode == "region_empty" || mode == "region_no_match" || mode == "region_no_allow" || mode == "region_unhealthy" {
				req.Selection.CountryCode = "JP"
			}
			if mode == "region_no_match" || mode == "region_no_allow" {
				candidates = []proxyPoolCandidate{codexPoolCandidate(1)}
				require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 1, &service.ProxyLatencyInfo{Success: true, CountryCode: "US", UpdatedAt: time.Now()}))
			}
			if mode == "region_no_allow" {
				req.AllowDirectOnEmpty = false
			}
			if mode == "unhealthy" || mode == "region_unhealthy" {
				candidates = []proxyPoolCandidate{codexPoolCandidate(1)}
				require.NoError(t, a.latencyCache.SetProxyLatency(ctx, 1, &service.ProxyLatencyInfo{Success: false, CountryCode: "JP", UpdatedAt: time.Now()}))
			}
			if mode == "capacity" {
				a.settings = proxyPoolSettingsStub{limit: 1}
				candidates = []proxyPoolCandidate{codexPoolCandidate(1, "8")}
			}
			if mode == "silent" {
				candidates = []proxyPoolCandidate{codexPoolCandidate(1)}
				other := codexRequest(8, req.Config)
				other.Selection.Restricted, other.Selection.IDs = true, []int64{1}
				r := codexStart(t, a, other)
				require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, false, true, false))
				codexFinish(t, a, r, "ticket_rejected")
			}
			a.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
				return candidates, nil
			}
			r, err := a.ReserveCodexTicket(ctx, req)
			if mode == "silent" {
				require.NoError(t, err)
				require.NotNil(t, r.Proxy, "cooldown recovery must not fall back to a direct connection")
				require.True(t, r.HalfOpen)
				codexFinish(t, a, r, "canceled")
				return
			}
			if mode == "empty" || mode == "region_empty" || mode == "region_no_match" {
				require.NoError(t, err)
				require.Nil(t, r.Proxy)
				require.NoError(t, a.StartCodexTicket(ctx, r))
				require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
				business, err := a.SelectCodexTicketBusinessProxy(ctx, req.Selection, r)
				require.NoError(t, err)
				require.Nil(t, business)
				require.NoError(t, a.CheckCodexTicketStage(ctx, r, business))
				codexFinish(t, a, r, "success")
				return
			}
			require.Error(t, err)
			require.Nil(t, r)
			if mode == "region_unhealthy" {
				requireCodexWait(t, err, "proxy_unhealthy")
			}
		})
	}
}
