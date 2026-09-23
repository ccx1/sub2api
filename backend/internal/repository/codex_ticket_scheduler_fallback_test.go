package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

type codexFallbackTest struct {
	allocator *ProxyPoolAllocator
	server    *miniredis.Miniredis
	request   service.CodexTicketReserveRequest
	selection service.ProxyPoolSelection
	role      string
	initial   int64
}

func newCodexFallbackTest(t *testing.T, role string) codexFallbackTest {
	t.Helper()
	a, server, req := newCodexPinTest(t, false)
	server.SetTime(time.Date(2026, time.September, 22, 0, 0, 0, 0, time.UTC))
	req.Config.ProxyFailureThreshold = 1
	codexSeedPins(t, a, req)
	req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
	fixture := codexFallbackTest{allocator: a, server: server, request: req, role: role, initial: 1}
	ctx := context.Background()
	r := codexStart(t, a, req)
	finish := service.CodexTicketFinishRequest{Reservation: r, Outcome: "upstream_error", HarvestProxyFailed: true}
	if role == "business" {
		fixture.initial = 2
		require.EqualValues(t, 2, codexPinBusiness(t, a, r).ID)
		finish.HarvestAccepted, finish.HarvestProxyFailed, finish.BusinessProxyFailed = true, false, true
		finish.Outcome = "verification_failed"
	}
	require.NoError(t, a.FinishCodexTicket(ctx, finish))
	fixture.selection = service.ProxyPoolSelection{AccountID: req.AccountID, Restricted: true, IDs: []int64{fixture.initial}}
	return fixture
}

func (fixture codexFallbackTest) try(t *testing.T, manual bool) (*service.Proxy, error) {
	t.Helper()
	ctx, req := context.Background(), fixture.request
	req.Manual = manual
	if fixture.role == "harvest" {
		req.Selection = fixture.selection
	}
	r, err := fixture.allocator.ReserveCodexTicket(ctx, req)
	if err != nil {
		return nil, err
	}
	require.NoError(t, fixture.allocator.StartCodexTicket(ctx, r))
	defer codexFinish(t, fixture.allocator, r, "canceled")
	if fixture.role == "harvest" {
		return r.Proxy, nil
	}
	require.NoError(t, fixture.allocator.ReportCodexTicketHarvest(ctx, r, true, false, false))
	proxy, err := fixture.allocator.SelectCodexTicketBusinessProxy(ctx, fixture.selection, r)
	if err == nil {
		require.NoError(t, fixture.allocator.CheckCodexTicketStage(ctx, r, proxy))
	}
	return proxy, err
}

func TestCodexSchedulerFallbackAutomaticSingleCandidateResumes(t *testing.T) {
	for _, role := range []string{"harvest", "business"} {
		t.Run(role, func(t *testing.T) {
			fixture := newCodexFallbackTest(t, role)
			ctx := context.Background()
			other := fixture.request
			other.AccountID, other.Selection.AccountID = 8, 8
			codexSeedPins(t, fixture.allocator, other)
			before := fixture.allocator.rdb.Get(ctx, codexSchedulerAccountKey(8)).Val()
			affinity := fixture.allocator.rdb.HGetAll(ctx, proxyPoolAffinityKey("8")).Val()
			_, err := fixture.try(t, false)
			status := requireCodexWait(t, err, "proxy_switch_waiting")
			require.NotNil(t, status.RetryAt)
			codexAdvance(t, fixture.allocator, fixture.server, time.Second)
			proxy, err := fixture.try(t, false)
			require.NoError(t, err)
			require.Equal(t, fixture.initial, proxy.ID)
			require.Equal(t, before, fixture.allocator.rdb.Get(ctx, codexSchedulerAccountKey(8)).Val())
			require.Equal(t, affinity, fixture.allocator.rdb.HGetAll(ctx, proxyPoolAffinityKey("8")).Val())
		})
	}
}

func TestCodexSchedulerFallbackPrefersAvailableAlternative(t *testing.T) {
	for _, role := range []string{"harvest", "business"} {
		t.Run(role, func(t *testing.T) {
			fixture := newCodexFallbackTest(t, role)
			fixture.selection.IDs = append(fixture.selection.IDs, 3)
			codexAdvance(t, fixture.allocator, fixture.server, time.Second)
			proxy, err := fixture.try(t, false)
			require.NoError(t, err)
			require.EqualValues(t, 3, proxy.ID, "an available replacement takes precedence even after the fallback deadline")
		})
	}
}

func TestCodexSchedulerFallbackUnavailableAlternativeResumes(t *testing.T) {
	for _, role := range []string{"harvest", "business"} {
		for _, blocked := range []string{"unhealthy", "capacity", "silent"} {
			t.Run(role+"/"+blocked, func(t *testing.T) {
				fixture := newCodexFallbackTest(t, role)
				fixture.selection.IDs = append(fixture.selection.IDs, 3)
				fixture.block(t, 3, blocked)
				_, err := fixture.try(t, false)
				requireCodexWait(t, err, "proxy_switch_waiting")
				codexAdvance(t, fixture.allocator, fixture.server, time.Second)
				proxy, err := fixture.try(t, false)
				require.NoError(t, err)
				require.Equal(t, fixture.initial, proxy.ID)
			})
		}
	}
}

func TestCodexSchedulerFallbackManualImmediatelyRetriesOriginal(t *testing.T) {
	for _, role := range []string{"harvest", "business"} {
		for _, alternative := range []bool{false, true} {
			name := role + "/single"
			if alternative {
				name = role + "/unavailable_alternative"
			}
			t.Run(name, func(t *testing.T) {
				fixture := newCodexFallbackTest(t, role)
				if alternative {
					fixture.selection.IDs = append(fixture.selection.IDs, 3)
					fixture.block(t, 3, "unhealthy")
				}
				proxy, err := fixture.try(t, true)
				require.NoError(t, err)
				require.Equal(t, fixture.initial, proxy.ID)
			})
		}
	}
}

func TestCodexSchedulerFallbackLegacyAvoidStateResumes(t *testing.T) {
	for _, role := range []string{"harvest", "business"} {
		t.Run(role, func(t *testing.T) {
			fixture := newCodexFallbackTest(t, role)
			ctx, key := context.Background(), codexSchedulerAccountKey(fixture.request.AccountID)
			var state map[string]any
			require.NoError(t, json.Unmarshal([]byte(fixture.allocator.rdb.Get(ctx, key).Val()), &state))
			delete(state, role+"avoiduntil")
			encoded, err := json.Marshal(state)
			require.NoError(t, err)
			require.NoError(t, fixture.allocator.rdb.Set(ctx, key, encoded, 0).Err())
			_, err = fixture.try(t, false)
			requireCodexWait(t, err, "proxy_switch_waiting")
			codexAdvance(t, fixture.allocator, fixture.server, time.Second)
			proxy, err := fixture.try(t, false)
			require.NoError(t, err)
			require.Equal(t, fixture.initial, proxy.ID)
		})
	}
}

func TestCodexSchedulerFallbackPreservesProxyGuards(t *testing.T) {
	for _, role := range []string{"harvest", "business"} {
		for _, blocked := range []string{"unhealthy", "capacity", "silent", "half_open_busy"} {
			t.Run(role+"/"+blocked, func(t *testing.T) {
				fixture := newCodexFallbackTest(t, role)
				fixture.block(t, fixture.initial, blocked)
				codexAdvance(t, fixture.allocator, fixture.server, time.Second)
				_, err := fixture.try(t, false)
				reason := map[string]string{"unhealthy": "proxy_unhealthy", "silent": "proxy_silent"}[blocked]
				if reason == "" {
					reason = blocked
				}
				requireCodexWait(t, err, reason)
				if blocked == "silent" {
					proxy, err := fixture.try(t, true)
					require.NoError(t, err, "manual retries bypass silence after falling back")
					require.Equal(t, fixture.initial, proxy.ID)
				} else {
					_, err := fixture.try(t, true)
					requireCodexWait(t, err, reason)
				}
			})
		}
	}
}

func TestCodexSchedulerFallbackPreservesAccountCooldown(t *testing.T) {
	for _, role := range []string{"harvest", "business"} {
		t.Run(role, func(t *testing.T) {
			fixture := newCodexFallbackTest(t, role)
			fixture.request.Config.Protection.Enabled = true
			fixture.request.Config.Protection.MaxAccountAttempts = 1
			ctx := context.Background()
			r := codexStart(t, fixture.allocator, fixture.request)
			finish := service.CodexTicketFinishRequest{Reservation: r, Outcome: "upstream_error", HarvestProxyFailed: true}
			if role == "business" {
				require.NoError(t, fixture.allocator.ReportCodexTicketHarvest(ctx, r, true, false, false))
				proxy, err := fixture.allocator.SelectCodexTicketBusinessProxy(ctx, fixture.selection, r)
				require.NoError(t, err)
				require.NoError(t, fixture.allocator.CheckCodexTicketStage(ctx, r, proxy))
				finish.HarvestAccepted, finish.HarvestProxyFailed, finish.BusinessProxyFailed = true, false, true
				finish.Outcome = "verification_failed"
			}
			require.NoError(t, fixture.allocator.FinishCodexTicket(ctx, finish))
			codexAdvance(t, fixture.allocator, fixture.server, time.Second)
			_, err := fixture.try(t, false)
			requireCodexWait(t, err, "account_cooldown")
			codexAdvance(t, fixture.allocator, fixture.server, 10*time.Second)
			proxy, err := fixture.try(t, false)
			require.NoError(t, err)
			require.Equal(t, fixture.initial, proxy.ID)
		})
	}
}

func (fixture codexFallbackTest) block(t *testing.T, id int64, reason string) {
	t.Helper()
	ctx := context.Background()
	switch reason {
	case "unhealthy":
		require.NoError(t, fixture.allocator.latencyCache.SetProxyLatency(ctx, id, &service.ProxyLatencyInfo{Success: false, UpdatedAt: time.Now()}))
	case "capacity":
		fixture.allocator.settings = proxyPoolSettingsStub{limit: 1}
		fixture.allocator.loadCandidates = func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
			candidates := []proxyPoolCandidate{codexPoolCandidate(1), codexPoolCandidate(2), codexPoolCandidate(3)}
			candidates[id-1] = codexPoolCandidate(id, "99")
			return candidates, nil
		}
	case "silent", "half_open_busy":
		cfg := fixture.request.Config
		protection := cfg.TicketProtection()
		protection.Enabled, protection.ProxySilenceSeconds = true, 300
		cfg.Protection = &protection
		other := codexRequest(99, cfg)
		other.Manual = true
		other.Selection.Restricted, other.Selection.IDs = true, []int64{id}
		codexReject(t, fixture.allocator, codexStart(t, fixture.allocator, other))
		if reason == "half_open_busy" {
			half := codexStart(t, fixture.allocator, other)
			t.Cleanup(func() { codexFinish(t, fixture.allocator, half, "canceled") })
		}
	}
}
