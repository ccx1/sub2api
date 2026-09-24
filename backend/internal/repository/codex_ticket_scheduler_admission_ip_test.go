package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func setCodexCurrentIP(t *testing.T, a *ProxyPoolAllocator, proxy *service.Proxy, ip string, at time.Time) {
	t.Helper()
	require.NoError(t, a.latencyCache.SetProxyLatency(context.Background(), proxy.ID, &service.ProxyLatencyInfo{
		Success: true, IPAddress: ip, UpdatedAt: at, ProxyIdentity: service.ProxyProbeIdentity(proxy),
	}))
}

func TestCodexSchedulerStartRejectsIPDisabledAfterReservation(t *testing.T) {
	candidate := codexIPCandidate(1, "rotating.example")
	f := newCodexIPTest(t, candidate)
	ctx := context.Background()
	req := f.request(1, 1)
	now, err := f.a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	setCodexCurrentIP(t, f.a, candidate.proxy, "203.0.113.10", now)
	reserved, err := f.a.ReserveCodexTicket(ctx, req)
	require.NoError(t, err)
	seedProxyIPGuard(t, f.a, "203.0.113.10", true, 0)
	err = f.a.StartCodexTicket(ctx, reserved)
	requireCodexWait(t, err, "ip_disabled")
}

func TestCodexSchedulerStartRejectsNewDisabledEgressAfterRotation(t *testing.T) {
	candidate := codexIPCandidate(1, "rotating.example")
	f := newCodexIPTest(t, candidate)
	ctx := context.Background()
	req := f.request(1, 1)
	now, err := f.a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	setCodexCurrentIP(t, f.a, candidate.proxy, "203.0.113.10", now)
	reserved, err := f.a.ReserveCodexTicket(ctx, req)
	require.NoError(t, err)
	setCodexCurrentIP(t, f.a, candidate.proxy, "203.0.113.11", now.Add(time.Second))
	seedProxyIPGuard(t, f.a, "203.0.113.11", true, 0)
	err = f.a.StartCodexTicket(ctx, reserved)
	requireCodexWait(t, err, "ip_disabled")
}

func TestCodexSchedulerValidationChecksNewEgressWithoutChangingHarvestAttribution(t *testing.T) {
	candidate := codexIPCandidate(1, "rotating.example")
	f := newCodexIPTest(t, candidate)
	ctx := context.Background()
	req := f.request(1, 1)
	now, err := f.a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	setCodexCurrentIP(t, f.a, candidate.proxy, "203.0.113.10", now)
	reservation := codexStart(t, f.a, req)
	setCodexCurrentIP(t, f.a, candidate.proxy, "203.0.113.11", now.Add(time.Second))
	seedProxyIPGuard(t, f.a, "203.0.113.11", true, 0)
	err = f.a.ValidateCodexTicket(ctx, reservation)
	requireCodexWait(t, err, "ip_disabled")
	require.NoError(t, f.a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
		Reservation: reservation, Outcome: "upstream_error", HarvestProxyFailed: true,
	}))
	var oldState struct {
		Failed map[string]int64 `json:"failed"`
	}
	raw, err := f.a.rdb.Get(ctx, proxyIPGuardKey("203.0.113.10")).Bytes()
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &oldState))
	require.Contains(t, oldState.Failed, "1", "failure attribution remains on the IP used at Start")
	var newState struct {
		Failed map[string]int64 `json:"failed"`
	}
	raw, err = f.a.rdb.Get(ctx, proxyIPGuardKey("203.0.113.11")).Bytes()
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &newState))
	require.NotContains(t, newState.Failed, "1")
}
