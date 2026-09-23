package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexSchedulerQualityFailureRejectsMintExitImmediately(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, follow := range []bool{false, true} {
			t.Run("protection="+strconv.FormatBool(enabled)+"/follow="+strconv.FormatBool(follow), func(t *testing.T) {
				a, _, req := newCodexPinTest(t, enabled)
				req.Config.BusinessVerificationRounds, req.Config.ProxyFailureThreshold = 5, 100
				if !follow {
					codexSeedPins(t, a, req)
				}
				req.HarvestUsesBusiness = follow
				req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
				ctx := context.Background()
				r := codexStart(t, a, req)
				require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
				businessBefore := a.rdb.HGetAll(ctx, proxyPoolAffinityKey("7")).Val()
				require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
					Reservation: r, HarvestAccepted: true, QualityProxyFailed: true, Outcome: "verification_failed",
				}))
				if follow {
					require.Empty(t, a.rdb.HGetAll(ctx, proxyPoolAffinityKey("7")).Val())
				} else {
					require.Equal(t, businessBefore, a.rdb.HGetAll(ctx, proxyPoolAffinityKey("7")).Val())
				}
				req.Selection.Restricted, req.Selection.IDs = false, nil
				next := codexStart(t, a, req)
				require.NotEqualValues(t, 1, next.Proxy.ID, "一次质量失败即换采票出口，不受普通失败阈值影响")
				if !follow {
					require.EqualValues(t, 2, codexPinBusiness(t, a, next).ID, "质量失败不能淘汰独立业务出口")
				}
				codexFinish(t, a, next, "canceled")
			})
		}
	}
}

func TestCodexSchedulerQualityFailureNeutralCompletionKeepsMintExit(t *testing.T) {
	for _, outcome := range []string{"canceled", "cancelled", "controls_changed", "verification_deferred"} {
		t.Run(outcome, func(t *testing.T) {
			a, _, req := newCodexPinTest(t, true)
			req.Config.BusinessVerificationRounds = 5
			codexSeedPins(t, a, req)
			ctx := context.Background()
			r := codexStart(t, a, req)
			require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
			require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
				Reservation: r, HarvestAccepted: true, QualityProxyFailed: true, Outcome: outcome,
			}))
			require.Zero(t, r.Status.AttemptsUsed)
			next := codexStart(t, a, req)
			require.EqualValues(t, 1, next.Proxy.ID)
			codexFinish(t, a, next, "canceled")
		})
	}
}

func TestCodexSchedulerQualityFailureFencesLateCompletion(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	req.Config.BusinessVerificationRounds = 5
	codexSeedPins(t, a, req)
	ctx := context.Background()
	old := codexStart(t, a, req)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, old, true, false, false))
	finish := service.CodexTicketFinishRequest{
		Reservation: old, HarvestAccepted: true, QualityProxyFailed: true, Outcome: "verification_failed",
	}
	require.NoError(t, a.FinishCodexTicket(ctx, finish))
	current := codexStart(t, a, req)
	before := a.rdb.Get(ctx, codexSchedulerAccountKey(req.AccountID)).Val()
	require.NoError(t, a.FinishCodexTicket(ctx, finish))
	require.Equal(t, before, a.rdb.Get(ctx, codexSchedulerAccountKey(req.AccountID)).Val())
	require.NoError(t, a.ValidateCodexTicket(ctx, current))
	codexFinish(t, a, current, "canceled")
}

func TestCodexSchedulerQualityFailureFencesChangedPolicy(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	req.Config.BusinessVerificationRounds = 5
	settings := &codexSchedulerSettingsStub{cfg: req.Config}
	a.settings = settings
	codexSeedPins(t, a, req)
	ctx := context.Background()
	r := codexStart(t, a, req)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	settings.cfg.TTLSeconds++
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
		Reservation: r, HarvestAccepted: true, QualityProxyFailed: true, Outcome: "verification_failed",
	}))
	req.Config = settings.cfg
	next := codexStart(t, a, req)
	require.EqualValues(t, 1, next.Proxy.ID, "旧策略的收尾不能淘汰当前出口")
	codexFinish(t, a, next, "canceled")
}

func TestCodexSchedulerQualityModeKeepsOrdinaryBusinessFailureAttribution(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	req.Config.BusinessVerificationRounds, req.Config.ProxyFailureThreshold = 5, 1
	codexSeedPins(t, a, req)
	ctx := context.Background()
	r := codexStart(t, a, req)
	require.EqualValues(t, 2, codexPinBusiness(t, a, r).ID)
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
		Reservation: r, HarvestAccepted: true, BusinessProxyFailed: true, Outcome: "verification_failed",
	}))
	next := codexStart(t, a, req)
	require.EqualValues(t, 1, next.Proxy.ID, "独立业务出口失败不淘汰已通过质量复验的采票出口")
	require.NotEqualValues(t, 2, codexPinBusiness(t, a, next).ID)
	codexFinish(t, a, next, "canceled")
}

func TestCodexSchedulerQualityFailureWithoutStartDoesNotRejectPin(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	req.Config.BusinessVerificationRounds = 5
	codexSeedPins(t, a, req)
	ctx := context.Background()
	r, err := a.ReserveCodexTicket(ctx, req)
	require.NoError(t, err)
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
		Reservation: r, QualityProxyFailed: true, Outcome: "verification_failed",
	}))
	next := codexStart(t, a, req)
	require.EqualValues(t, 1, next.Proxy.ID)
	codexFinish(t, a, next, "canceled")
}

func TestCodexSchedulerQualityFailureKeepsExplicitFixedProxy(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	req.Config.BusinessVerificationRounds = 5
	req.PoolMode, req.FixedProxy = false, codexPoolCandidate(1).proxy
	ctx := context.Background()
	r := codexStart(t, a, req)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
		Reservation: r, HarvestAccepted: true, QualityProxyFailed: true, Outcome: "verification_failed",
	}))
	next := codexStart(t, a, req)
	require.EqualValues(t, 1, next.Proxy.ID, "管理员固定出口没有可自动切换的候选")
	codexFinish(t, a, next, "canceled")
}

func TestCodexSchedulerQualityLeaseCoversSeriesAndBusinessVerification(t *testing.T) {
	a, server, req := newCodexPinTest(t, false)
	req.Config.BusinessVerificationRounds, req.Config.HarvestAttemptTimeoutSeconds = 5, 25
	req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
	ctx := context.Background()
	r := codexStart(t, a, req)
	codexAdvance(t, a, server, 25*time.Second)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	for range 5 {
		require.NoError(t, a.ValidateCodexTicket(ctx, r))
		codexAdvance(t, a, server, 25*time.Second)
	}
	require.NoError(t, a.CheckCodexTicketStage(ctx, r, codexPoolCandidate(2).proxy))
	codexAdvance(t, a, server, 25*time.Second)
	require.NoError(t, a.ValidateCodexTicket(ctx, r), "完整采集、连续质量复验和独立业务复验仍在同一租约内")
	require.NoError(t, a.rdb.ZScore(ctx, "proxy:{pool}:codex:leases:1", "7").Err())
	require.NoError(t, a.rdb.ZScore(ctx, "proxy:{pool}:codex:leases:2", "7").Err())
	codexFinish(t, a, r, "success")
	require.Zero(t, a.rdb.ZCard(ctx, "proxy:{pool}:codex:leases:1").Val())
	require.Zero(t, a.rdb.ZCard(ctx, "proxy:{pool}:codex:leases:2").Val())
}

func TestCodexSchedulerQualityLeaseExpiresAndKeepsDefaultDuration(t *testing.T) {
	for _, item := range []struct {
		name   string
		rounds int
		verify bool
		lease  time.Duration
	}{
		{"default", 1, true, 105 * time.Second},
		{"quality", 5, true, 205 * time.Second},
		{"maximum", 10, true, 330 * time.Second},
		{"disabled", 5, false, 105 * time.Second},
	} {
		t.Run(item.name, func(t *testing.T) {
			a, server, req := newCodexPinTest(t, false)
			req.Config.BusinessVerificationRounds, req.Config.VerifyBusiness = item.rounds, &item.verify
			req.Config.HarvestAttemptTimeoutSeconds = 25
			ctx := context.Background()
			r := codexStart(t, a, req)
			codexAdvance(t, a, server, item.lease-time.Second)
			require.NoError(t, a.ValidateCodexTicket(ctx, r))
			codexAdvance(t, a, server, 2*time.Second)
			requireCodexWait(t, a.ValidateCodexTicket(ctx, r), "reservation_expired")
		})
	}
}
