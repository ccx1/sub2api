package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

type codexIPTest struct {
	a      *ProxyPoolAllocator
	server *miniredis.Miniredis
	cfg    config.OpenAICodexTicketConfig
}

func newCodexIPTest(t *testing.T, candidates ...proxyPoolCandidate) codexIPTest {
	t.Helper()
	if len(candidates) == 0 {
		candidates = []proxyPoolCandidate{codexIPCandidate(1, "203.0.113.10")}
	}
	a, server := newProxyPoolAllocatorTest(t, 0, candidates...)
	server.SetTime(time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC))
	cfg := codexSchedulerConfig()
	cfg.ProxyFailureThreshold = 1000
	cfg.Protection.Enabled = false
	cfg.Protection.ProxyIPProtectionEnabled = true
	cfg.Protection.ProxyIPFailureAccountThreshold = 2
	cfg.Protection.ProxyIPCooldownSeconds = 5
	cfg.Protection.ProxyIPMaxRounds = 2
	cfg.Protection.ProxyIPFailureWindowSeconds = 60
	return codexIPTest{a: a, server: server, cfg: cfg}
}

func codexIPCandidate(id int64, ip string) proxyPoolCandidate {
	candidate := codexPoolCandidate(id)
	candidate.proxy.Host = ip
	return candidate
}

func (f codexIPTest) request(accountID, proxyID int64) service.CodexTicketReserveRequest {
	req := codexRequest(accountID, f.cfg)
	req.Manual = true
	if proxyID > 0 {
		req.Selection.Restricted, req.Selection.IDs = true, []int64{proxyID}
	}
	return req
}

func (f codexIPTest) fail(t *testing.T, req service.CodexTicketReserveRequest) {
	t.Helper()
	f.finish(t, service.CodexTicketFinishRequest{
		Reservation: codexStart(t, f.a, req), Outcome: "upstream_error", HarvestProxyFailed: true,
	})
}

func (f codexIPTest) finish(t *testing.T, request service.CodexTicketFinishRequest) {
	t.Helper()
	require.NoError(t, f.a.FinishCodexTicket(context.Background(), request))
}

func (f codexIPTest) available(t *testing.T, req service.CodexTicketReserveRequest) {
	t.Helper()
	codexFinish(t, f.a, codexStart(t, f.a, req), "canceled")
}

func (f codexIPTest) blocked(t *testing.T, req service.CodexTicketReserveRequest, reason string) {
	t.Helper()
	r, err := f.a.ReserveCodexTicket(context.Background(), req)
	require.Nil(t, r)
	requireCodexWait(t, err, reason)
}

func TestCodexSchedulerIPDistinctAccountsAcrossModelsAndProxyRows(t *testing.T) {
	f := newCodexIPTest(t, codexIPCandidate(1, "203.0.113.10"), codexIPCandidate(2, "203.0.113.10"), codexIPCandidate(3, "203.0.113.20"))
	f.fail(t, f.request(1, 1))
	repeated := f.request(1, 2)
	repeated.Model, repeated.Models = "sol", []string{"sol"}
	f.fail(t, repeated)
	f.available(t, f.request(2, 1))
	f.fail(t, f.request(2, 2))
	for _, manual := range []bool{false, true} {
		req := f.request(3, 0)
		req.Selection.Restricted, req.Selection.IDs = true, []int64{1, 2}
		req.Manual = manual
		f.blocked(t, req, "ip_cooling")
	}
	f.available(t, f.request(4, 3))
}

func TestCodexSchedulerIPCooldownExpiresAndEachRoundRequiresDistinctAccounts(t *testing.T) {
	f := newCodexIPTest(t)
	f.fail(t, f.request(1, 1))
	f.fail(t, f.request(2, 1))
	codexAdvance(t, f.a, f.server, 5*time.Second-time.Millisecond)
	f.blocked(t, f.request(3, 1), "ip_cooling")
	codexAdvance(t, f.a, f.server, time.Millisecond)
	f.fail(t, f.request(3, 1))
	f.fail(t, f.request(3, 1))
	f.available(t, f.request(4, 1))
	f.fail(t, f.request(4, 1))
	f.blocked(t, f.request(5, 1), "ip_disabled")
	codexAdvance(t, f.a, f.server, 24*time.Hour)
	f.blocked(t, f.request(5, 1), "ip_disabled")
}

func TestCodexSchedulerIPFailureWindowExpiresOldAccounts(t *testing.T) {
	f := newCodexIPTest(t)
	f.fail(t, f.request(1, 1))
	codexAdvance(t, f.a, f.server, 60*time.Second)
	f.fail(t, f.request(2, 1))
	f.available(t, f.request(3, 1))
	f.fail(t, f.request(3, 1))
	f.blocked(t, f.request(4, 1), "ip_cooling")
}

func TestCodexSchedulerIPUsesOnlyHarvestFailureAndSuccess(t *testing.T) {
	for _, tc := range []struct {
		name    string
		finish  service.CodexTicketFinishRequest
		counted bool
		reset   bool
	}{
		{name: "harvest failure", finish: service.CodexTicketFinishRequest{Outcome: "upstream_error", HarvestProxyFailed: true}, counted: true},
		{name: "quality failure", finish: service.CodexTicketFinishRequest{Outcome: "verification_failed", HarvestAccepted: true, QualityProxyFailed: true}, counted: true},
		{name: "business failure", finish: service.CodexTicketFinishRequest{Outcome: "verification_failed", BusinessProxyFailed: true}},
		{name: "business failed after harvest", finish: service.CodexTicketFinishRequest{Outcome: "verification_failed", HarvestAccepted: true, BusinessProxyFailed: true}, reset: true},
		{name: "existing ticket verification", finish: service.CodexTicketFinishRequest{Outcome: "success"}},
		{name: "success", finish: service.CodexTicketFinishRequest{Outcome: "success", HarvestAccepted: true}, reset: true},
		{name: "cancel", finish: service.CodexTicketFinishRequest{Outcome: "canceled", HarvestProxyFailed: true}},
		{name: "deferred", finish: service.CodexTicketFinishRequest{Outcome: "verification_deferred", HarvestProxyFailed: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCodexIPTest(t)
			f.fail(t, f.request(1, 1))
			tc.finish.Reservation = codexStart(t, f.a, f.request(2, 1))
			f.finish(t, tc.finish)
			if tc.counted {
				f.blocked(t, f.request(3, 1), "ip_cooling")
				return
			}
			f.fail(t, f.request(3, 1))
			if tc.reset {
				f.available(t, f.request(4, 1))
			} else {
				f.blocked(t, f.request(4, 1), "ip_cooling")
			}
		})
	}
}

func TestCodexSchedulerIPSuccessfulHarvestResetsFailedRounds(t *testing.T) {
	f := newCodexIPTest(t)
	f.fail(t, f.request(1, 1))
	f.fail(t, f.request(2, 1))
	codexAdvance(t, f.a, f.server, 5*time.Second)
	f.finish(t, service.CodexTicketFinishRequest{Reservation: codexStart(t, f.a, f.request(3, 1)), HarvestAccepted: true, Outcome: "success"})
	f.fail(t, f.request(4, 1))
	f.fail(t, f.request(5, 1))
	f.blocked(t, f.request(6, 1), "ip_cooling")
}

func TestCodexSchedulerIPQualityFailureKeepsActualHarvestAttribution(t *testing.T) {
	f := newCodexIPTest(t, codexIPCandidate(1, "203.0.113.10"), codexIPCandidate(2, "203.0.113.20"))
	f.fail(t, f.request(1, 1))
	ctx := context.Background()
	r := codexStart(t, f.a, f.request(2, 1))
	require.NoError(t, f.a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	selection := service.ProxyPoolSelection{AccountID: 2, Restricted: true, IDs: []int64{2}}
	business, err := f.a.SelectCodexTicketBusinessProxy(ctx, selection, r)
	require.NoError(t, err)
	require.EqualValues(t, 2, business.ID)
	require.NoError(t, f.a.CheckCodexTicketStage(ctx, r, business))
	f.finish(t, service.CodexTicketFinishRequest{
		Reservation: r, HarvestAccepted: true, QualityProxyFailed: true, Outcome: "verification_failed",
	})
	f.blocked(t, f.request(3, 1), "ip_cooling")
	f.available(t, f.request(3, 2))
}

func TestCodexSchedulerIPUsesMatchingProbeIdentityAndCanonicalAddress(t *testing.T) {
	for _, tc := range []struct {
		name, observed, host, identity string
		want                           string
	}{
		{name: "observed IPv4", observed: " 203.0.113.10 ", host: "pool.invalid", identity: "current", want: "203.0.113.10"},
		{name: "canonical IPv6", observed: "2001:0db8:0:0:0:0:0:1", host: "pool.invalid", identity: "current", want: "2001:db8::1"},
		{name: "literal fallback", host: "[2001:db8::2]", want: "2001:db8::2"},
		{name: "unknown host", host: "pool.invalid"},
		{name: "invalid observation", observed: "not-an-ip", host: "pool.invalid", identity: "current"},
		{name: "stale endpoint observation", observed: "203.0.113.10", host: "pool.invalid", identity: "old"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := codexIPCandidate(1, tc.host)
			f := newCodexIPTest(t, candidate)
			if tc.observed != "" {
				identity := tc.identity
				if identity == "current" {
					identity = service.ProxyProbeIdentity(candidate.proxy)
				}
				require.NoError(t, f.a.latencyCache.SetProxyLatency(context.Background(), 1, &service.ProxyLatencyInfo{
					Success: true, IPAddress: tc.observed, ProxyIdentity: identity, UpdatedAt: time.Now(),
				}))
			}
			candidates, _, err := f.a.codexSchedulerCandidates(context.Background(), f.request(1, 1))
			require.NoError(t, err)
			require.Len(t, candidates, 1)
			require.Equal(t, tc.want, candidates[0].IP)
		})
	}
}

func TestCodexSchedulerIPUnknownDomainsDoNotAggregate(t *testing.T) {
	f := newCodexIPTest(t, codexPoolCandidate(1), codexPoolCandidate(2))
	for id := int64(1); id <= 6; id++ {
		f.fail(t, f.request(id, 1+id%2))
	}
	f.available(t, f.request(7, 1))
	f.available(t, f.request(8, 2))
}
