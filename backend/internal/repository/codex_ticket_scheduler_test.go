package repository

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

func codexSchedulerConfig() config.OpenAICodexTicketConfig {
	p := config.DefaultCodexTicketProtection()
	p.Enabled, p.ProxySilenceSeconds, p.AccountCooldownSeconds = true, 5, 10
	return config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{Enabled: true, Protection: &p,
		HarvestAttemptTimeoutSeconds: 1, HarvestProbeIntervalSeconds: 1, RetryBackoffSeconds: []int{1}, RetryMaxAttempts: 0})
}

func codexPoolCandidate(id int64, fixed ...string) proxyPoolCandidate {
	return proxyPoolCandidate{proxy: &service.Proxy{ID: id, Status: service.StatusActive, Protocol: "http", Host: fmt.Sprintf("proxy-%d.invalid", id), Port: 8080}, fixedIDs: fixed}
}

func codexRequest(account int64, cfg config.OpenAICodexTicketConfig) service.CodexTicketReserveRequest {
	return service.CodexTicketReserveRequest{AccountID: account, Model: "astra", Models: []string{"astra"},
		Selection: service.ProxyPoolSelection{AccountID: account}, PoolMode: true, Strategy: "round_robin", Config: cfg}
}

func codexAdvance(t *testing.T, a *ProxyPoolAllocator, server *miniredis.Miniredis, elapsed time.Duration) {
	t.Helper()
	now, err := a.rdb.Time(context.Background()).Result()
	require.NoError(t, err)
	server.SetTime(now.Add(elapsed))
	server.FastForward(elapsed)
}

func requireCodexWait(t *testing.T, err error, reason string) *service.CodexTicketRuntimeStatus {
	t.Helper()
	var wait *service.CodexTicketWaitError
	require.ErrorAs(t, err, &wait)
	require.Equal(t, reason, wait.Status.Reason)
	return wait.Status
}

func codexStart(t *testing.T, a *ProxyPoolAllocator, req service.CodexTicketReserveRequest) *service.CodexTicketReservation {
	t.Helper()
	r, err := a.ReserveCodexTicket(context.Background(), req)
	require.NoError(t, err)
	require.NoError(t, a.StartCodexTicket(context.Background(), r))
	return r
}

func codexFinish(t *testing.T, a *ProxyPoolAllocator, r *service.CodexTicketReservation, outcome string) {
	t.Helper()
	require.NoError(t, a.FinishCodexTicket(context.Background(), service.CodexTicketFinishRequest{Reservation: r, Outcome: outcome}))
}

func TestCodexSchedulerThresholdRotationDoesNotChangeBusinessAffinityOrChargeBeforeStart(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2), codexPoolCandidate(3))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	cfg.Protection.Enabled = false
	cfg.ProxyFailureThreshold = 1
	_, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	before := a.rdb.HGetAll(ctx, proxyPoolAffinityKey("7")).Val()
	req := codexRequest(7, cfg)
	reserved, err := a.ReserveCodexTicket(ctx, req)
	require.NoError(t, err)
	codexFinish(t, a, reserved, "canceled")
	require.Zero(t, reserved.Status.AttemptsUsed)
	r := codexStart(t, a, req)
	last := r.Proxy.ID
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: r, Outcome: "upstream_error", HarvestProxyFailed: true}))
	for range 2 {
		r = codexStart(t, a, req)
		require.NotEqual(t, last, r.Proxy.ID)
		last = r.Proxy.ID
		require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: r, Outcome: "upstream_error", HarvestProxyFailed: true}))
	}
	require.Equal(t, before, a.rdb.HGetAll(ctx, proxyPoolAffinityKey("7")).Val())
	status, err := a.GetCodexTicketRuntimeStatus(ctx, 7)
	require.NoError(t, err)
	require.Zero(t, status.AttemptsUsed)
}

func TestCodexSchedulerSharesUniqueAccountCapacityWithBusiness(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, codexPoolCandidate(1, "7"))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	r := codexStart(t, a, codexRequest(7, cfg))
	business, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.NotNil(t, business)
	other, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 8})
	require.NoError(t, err)
	require.Nil(t, other)
	_, err = a.ReserveCodexTicket(ctx, codexRequest(8, cfg))
	requireCodexWait(t, err, "capacity")
	codexFinish(t, a, r, "success")
	require.Equal(t, "1", a.rdb.HGet(ctx, proxyPoolAffinityKey("7"), "proxy_id").Val())
	require.Zero(t, a.rdb.ZCard(ctx, "proxy:{pool}:codex:leases:1").Val())
}

func TestCodexSchedulerSharedSilenceAndSingleHalfOpen(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	r := codexStart(t, a, codexRequest(1, cfg))
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, false, true, false))
	codexFinish(t, a, r, "ticket_rejected")
	action, err := a.GetCodexTicketRuntimeStatus(ctx, 1)
	require.NoError(t, err)
	require.True(t, action.RuleMatched)
	require.NotNil(t, action.SilenceUntil)
	require.NotEmpty(t, action.PolicyVersion)
	req := codexRequest(2, cfg)
	req.PoolMode, req.FixedProxy, req.Model, req.Models = false, codexPoolCandidate(1).proxy, "sol", []string{"sol"}
	_, err = a.ReserveCodexTicket(ctx, req)
	status := requireCodexWait(t, err, "proxy_silent")
	require.Zero(t, status.AttemptsUsed)
	codexAdvance(t, a, server, 6*time.Second)
	half := codexStart(t, a, req)
	require.True(t, half.HalfOpen)
	_, err = a.ReserveCodexTicket(ctx, codexRequest(3, cfg))
	requireCodexWait(t, err, "half_open_busy")
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, half, true, false, false))
	require.NoError(t, a.CheckCodexTicketStage(ctx, half, half.Proxy))
	codexFinish(t, a, half, "success")
	action, err = a.GetCodexTicketRuntimeStatus(ctx, 2)
	require.NoError(t, err)
	require.True(t, action.HarvestHalfOpen)
	require.True(t, action.HarvestAccepted)
	require.False(t, action.RuleMatched)
	next := codexStart(t, a, codexRequest(3, cfg))
	require.False(t, next.HalfOpen)
	codexFinish(t, a, next, "success")
}

func TestCodexSchedulerAccountBudgetSharedByModelsAndLastAttemptCompletes(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	req := codexRequest(7, cfg)
	req.PoolMode, req.FixedProxy, req.Manual = false, codexPoolCandidate(1).proxy, true
	var generation int64
	for i := 0; i < 6; i++ {
		req.Model, req.Models = "model-"+strconv.Itoa(i%2), []string{"model-" + strconv.Itoa(i%2)}
		r := codexStart(t, a, req)
		generation = r.Generation
		require.NoError(t, a.StartCodexTicket(ctx, r), "duplicate Start cannot double-charge")
		require.NoError(t, a.CheckCodexTicketStage(ctx, r, r.Proxy), "last attempt may finish verification")
		codexFinish(t, a, r, "upstream_error")
		require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: r, Outcome: "upstream_error"}))
	}
	req.Manual = false
	_, err := a.ReserveCodexTicket(ctx, req)
	status := requireCodexWait(t, err, "account_cooldown")
	require.Equal(t, 6, status.AttemptsUsed)
	codexAdvance(t, a, server, 11*time.Second)
	r := codexStart(t, a, req)
	require.Greater(t, r.Generation, generation)
	codexFinish(t, a, r, "success")
}

func TestCodexSchedulerTwoRoundsAndPoolReplacementKeepBudget(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	cfg.ProxyFailureThreshold = 1
	req := codexRequest(7, cfg)
	req.Manual = true
	seen := []int64{}
	for range 4 {
		r := codexStart(t, a, req)
		seen = append(seen, r.Proxy.ID)
		require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: r, Outcome: "upstream_error", HarvestProxyFailed: true}))
	}
	require.NotEqual(t, seen[0], seen[1])
	require.Equal(t, seen[:2], seen[2:])
	req.Manual = false
	_, err := a.ReserveCodexTicket(ctx, req)
	require.Equal(t, 4, requireCodexWait(t, err, "account_cooldown").AttemptsUsed)
	req.Manual = true
	b, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	first := codexStart(t, b, req)
	codexFinish(t, b, first, "upstream_error")
	b.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
		return []proxyPoolCandidate{codexPoolCandidate(2)}, nil
	}
	second := codexStart(t, b, req)
	require.Equal(t, first.Generation, second.Generation)
	require.EqualValues(t, 2, second.Proxy.ID)
	codexFinish(t, b, second, "upstream_error")
	status, err := b.GetCodexTicketRuntimeStatus(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, 2, status.AttemptsUsed)
}

func TestCodexSchedulerReadOnlyStatusAndRedisFailure(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx := context.Background()
	r := codexStart(t, a, codexRequest(7, codexSchedulerConfig()))
	before := a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val()
	_, err := a.GetCodexTicketRuntimeStatus(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, before, a.rdb.Get(ctx, codexSchedulerAccountKey(7)).Val())
	server.Close()
	require.Error(t, a.StartCodexTicket(ctx, r))
	_, err = a.ReserveCodexTicket(ctx, codexRequest(8, codexSchedulerConfig()))
	require.Error(t, err)
}
