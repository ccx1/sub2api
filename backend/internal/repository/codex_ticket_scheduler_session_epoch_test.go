package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func codexSessionEpoch(t *testing.T, a *ProxyPoolAllocator, req service.CodexTicketReserveRequest) string {
	t.Helper()
	epoch, err := a.GetCodexTicketSessionEpoch(context.Background(), service.CodexTicketSessionScope{
		AccountID: req.AccountID, Model: req.Model, Mode: req.Config.SessionMode,
	})
	require.NoError(t, err)
	return epoch
}

func codexSessionFail(t *testing.T, a *ProxyPoolAllocator, r *service.CodexTicketReservation) {
	t.Helper()
	require.NoError(t, a.FinishCodexTicket(context.Background(), service.CodexTicketFinishRequest{
		Reservation: r, HarvestProxyFailed: true, Outcome: "upstream_error",
	}))
}

func TestCodexSchedulerSessionEpochReadOnlyDoesNotCreateOrChangeReservation(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	ctx := context.Background()
	for _, mode := range []string{"account", "account_model"} {
		req.Config.SessionMode = mode
		require.Empty(t, codexSessionEpoch(t, a, req))
		require.Zero(t, a.rdb.DBSize(ctx).Val())
	}
	r, err := a.ReserveCodexTicket(ctx, req)
	require.NoError(t, err)
	before := a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val()
	for range 3 {
		require.Empty(t, codexSessionEpoch(t, a, req))
	}
	require.Equal(t, before, a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val())
	require.NoError(t, a.StartCodexTicket(ctx, r))
	codexSessionFail(t, a, r)
	require.Empty(t, codexSessionEpoch(t, a, req))
}

func TestCodexSchedulerSessionAccountSharesEpochAndCountsFailuresPerModel(t *testing.T) {
	a, server, req := newCodexPinTest(t, false)
	req.Config.SessionMode = "account"
	ctx := context.Background()
	var previous service.CodexTicketFinishRequest
	for i, model := range []string{"astra", "sol", "astra", "astra"} {
		req.Model, req.Models = model, []string{"astra", "sol"}
		r := codexStart(t, a, req)
		require.Empty(t, r.SessionEpoch)
		if i > 0 {
			require.NoError(t, a.FinishCodexTicket(ctx, previous))
		}
		previous = service.CodexTicketFinishRequest{Reservation: r, HarvestProxyFailed: true, Outcome: "upstream_error"}
		if i == 1 {
			codexPinBusiness(t, a, r)
			previous.HarvestProxyFailed, previous.HarvestAccepted, previous.BusinessProxyFailed = false, true, true
			previous.Outcome = "verification_failed"
		}
		require.NoError(t, a.FinishCodexTicket(ctx, previous))
		require.NoError(t, a.FinishCodexTicket(ctx, previous))
		require.Empty(t, r.SessionEpoch, "completed attempts retain the epoch used by both probes")
		if i < 3 {
			require.Empty(t, codexSessionEpoch(t, a, req))
		}
	}
	epoch := codexSessionEpoch(t, a, req)
	_, err := uuid.Parse(epoch)
	require.NoError(t, err)
	other := *a
	other.rdb = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = other.rdb.Close() })
	req.Model = "sol"
	r, err := other.ReserveCodexTicket(ctx, req)
	require.NoError(t, err)
	require.Equal(t, epoch, r.SessionEpoch)
	codexFinish(t, &other, r, "canceled")
	require.Equal(t, epoch, codexSessionEpoch(t, a, req))
}

func TestCodexSchedulerSessionAccountModelIsolatesFailureEpochs(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	req.Config.SessionMode, req.Config.ProxyFailureThreshold = "account_model", 2
	for _, model := range []string{"astra", "sol", "astra"} {
		req.Model, req.Models = model, []string{"astra", "sol"}
		r := codexStart(t, a, req)
		require.Empty(t, r.SessionEpoch)
		codexSessionFail(t, a, r)
	}
	astraEpoch := codexSessionEpoch(t, a, req)
	require.NotEmpty(t, astraEpoch)
	req.Model = "sol"
	require.Empty(t, codexSessionEpoch(t, a, req))
	r := codexStart(t, a, req)
	require.Empty(t, r.SessionEpoch)
	codexSessionFail(t, a, r)
	solEpoch := codexSessionEpoch(t, a, req)
	require.NotEmpty(t, solEpoch)
	require.NotEqual(t, astraEpoch, solEpoch)
	req.Model = "astra"
	require.Equal(t, astraEpoch, codexSessionEpoch(t, a, req))
}

func TestCodexSchedulerSessionNeutralDoesNotCountAndSuccessResetsFailures(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	req.Config.SessionMode = "account"
	ctx := context.Background()
	codexSessionFail(t, a, codexStart(t, a, req))
	for _, outcome := range []string{"canceled", "controls_changed", "unauthorized", "forbidden", "rate_limited"} {
		r := codexStart(t, a, req)
		codexFinish(t, a, r, outcome)
		require.Empty(t, codexSessionEpoch(t, a, req))
	}
	codexSessionFail(t, a, codexStart(t, a, req))
	require.Empty(t, codexSessionEpoch(t, a, req))
	codexSessionFail(t, a, codexStart(t, a, req))
	firstEpoch := codexSessionEpoch(t, a, req)
	require.NotEmpty(t, firstEpoch, "neutral outcomes preserve the existing failure count")
	codexSessionFail(t, a, codexStart(t, a, req))
	r := codexStart(t, a, req)
	codexPinBusiness(t, a, r)
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
		Reservation: r, HarvestAccepted: true, BusinessProxySucceeded: true, Outcome: "success", TargetsComplete: true,
	}))
	for range 2 {
		codexSessionFail(t, a, codexStart(t, a, req))
		require.Equal(t, firstEpoch, codexSessionEpoch(t, a, req))
	}
	codexSessionFail(t, a, codexStart(t, a, req))
	require.NotEqual(t, firstEpoch, codexSessionEpoch(t, a, req))
}

func TestCodexSchedulerSessionFailuresSurviveCyclesRestartAndConfigSave(t *testing.T) {
	a, server, req := newCodexPinTest(t, true)
	req.Config.SessionMode = "account"
	req.Config.Protection.MaxAccountAttempts = 1
	var generation int64
	for i := 0; i < 3; i++ {
		if i > 0 {
			codexAdvance(t, a, server, 11*time.Second)
			copy := *a
			copy.rdb = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
			t.Cleanup(func() { _ = copy.rdb.Close() })
			a = &copy
			req.Config.HarvestProbeIntervalSeconds++
		}
		r := codexStart(t, a, req)
		require.Greater(t, r.Generation, generation)
		require.Empty(t, r.SessionEpoch)
		generation = r.Generation
		codexSessionFail(t, a, r)
		if i < 2 {
			require.Empty(t, codexSessionEpoch(t, a, req))
		}
	}
	require.NotEmpty(t, codexSessionEpoch(t, a, req))
}
