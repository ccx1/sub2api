package repository

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func codexLearningKey(account int64, model, role string, proxy *service.Proxy) string {
	return fmt.Sprintf("proxy:{pool}:codex:learning:%d:%x:%s:%d:%s", account, sha1.Sum([]byte(model)), role, proxy.ID, codexSchedulerVersion(proxy.URL()))
}

func codexLearningStats(t *testing.T, a *ProxyPoolAllocator, account int64, model, role string, proxy *service.Proxy) map[string]any {
	t.Helper()
	raw, err := a.rdb.Get(context.Background(), codexLearningKey(account, model, role, proxy)).Bytes()
	require.NoError(t, err)
	var stats map[string]any
	require.NoError(t, json.Unmarshal(raw, &stats))
	return stats
}

func TestCodexSchedulerLearningPersistsIsolatedStatsAndStageLatency(t *testing.T) {
	a, server, req := newCodexPinTest(t, false)
	server.SetTime(time.Unix(1700000000, 0))
	req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
	ctx := context.Background()
	r := codexStart(t, a, req)
	codexAdvance(t, a, server, 120*time.Millisecond)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	codexAdvance(t, a, server, time.Second)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	business := codexPoolCandidate(2).proxy
	require.NoError(t, a.CheckCodexTicketStage(ctx, r, business))
	codexAdvance(t, a, server, 240*time.Millisecond)
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
		Reservation: r, HarvestAccepted: true, BusinessProxySucceeded: true, Outcome: "success",
	}))
	other := *a
	other.rdb = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = other.rdb.Close() })
	stats := codexLearningStats(t, &other, 7, "astra", "harvest", r.Proxy)
	require.Equal(t, float64(1), stats["successes"])
	require.Equal(t, float64(120), stats["latency_ms"])
	require.Equal(t, float64(0), stats["consecutive_failures"])
	stats = codexLearningStats(t, &other, 7, "astra", "business", business)
	require.Equal(t, float64(240), stats["latency_ms"])
	for _, key := range []string{
		codexLearningKey(8, "astra", "harvest", r.Proxy),
		codexLearningKey(7, "sol", "harvest", r.Proxy),
		codexLearningKey(7, "astra", "business", r.Proxy),
	} {
		require.Zero(t, a.rdb.Exists(ctx, key).Val())
	}
	require.Equal(t, 30*24*time.Hour, a.rdb.PTTL(ctx, codexLearningKey(7, "astra", "harvest", r.Proxy)).Val())
}

func TestCodexSchedulerLearningCountsFailuresCooldownAndFencesDuplicates(t *testing.T) {
	a, server, req := newCodexPinTest(t, true)
	server.SetTime(time.Unix(1700000000, 0))
	req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
	ctx := context.Background()
	r := codexStart(t, a, req)
	codexAdvance(t, a, server, 80*time.Millisecond)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, false, true, false))
	finish := service.CodexTicketFinishRequest{Reservation: r, HarvestProxyFailed: true, Outcome: "ticket_rejected"}
	require.NoError(t, a.FinishCodexTicket(ctx, finish))
	stats := codexLearningStats(t, a, 7, "astra", "harvest", r.Proxy)
	require.Equal(t, float64(1), stats["failures"])
	require.Equal(t, float64(1), stats["consecutive_failures"])
	require.Equal(t, float64(80), stats["latency_ms"])
	require.Greater(t, stats["cooldown_until"].(float64), stats["updated_at"].(float64))
	require.NoError(t, a.FinishCodexTicket(ctx, finish))
	require.Equal(t, stats, codexLearningStats(t, a, 7, "astra", "harvest", r.Proxy))
	next := codexStart(t, a, req)
	require.True(t, next.HalfOpen, "学习记录不阻止全池最早节点恢复")
	require.NoError(t, a.FinishCodexTicket(ctx, finish))
	require.Equal(t, stats, codexLearningStats(t, a, 7, "astra", "harvest", r.Proxy))
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, next, true, false, false))
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: next, HarvestAccepted: true, Outcome: "success"}))
	stats = codexLearningStats(t, a, 7, "astra", "harvest", r.Proxy)
	require.Equal(t, float64(1), stats["successes"])
	require.Equal(t, float64(1), stats["failures"])
	require.Equal(t, float64(0), stats["consecutive_failures"])
}

func TestCodexSchedulerLearningIgnoresNeutralUnstartedAndPersistenceFailure(t *testing.T) {
	for _, outcome := range []string{"canceled", "controls_changed", "verification_deferred", "persistence_failed", "unstarted"} {
		t.Run(outcome, func(t *testing.T) {
			a, _, req := newCodexPinTest(t, false)
			req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
			ctx := context.Background()
			r, err := a.ReserveCodexTicket(ctx, req)
			require.NoError(t, err)
			if outcome != "unstarted" {
				require.NoError(t, a.StartCodexTicket(ctx, r))
			}
			require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
				Reservation: r, Outcome: outcome, HarvestProxyFailed: outcome != "persistence_failed",
			}))
			require.Zero(t, a.rdb.Exists(ctx, codexLearningKey(7, "astra", "harvest", r.Proxy)).Val())
		})
	}
}

func TestCodexSchedulerLearningRanksKnownNodesAndExploresUnknown(t *testing.T) {
	a, server, req := newCodexPinTest(t, false)
	req.PoolMode, req.FixedProxy = false, codexPoolCandidate(1).proxy
	ctx := context.Background()
	for range 3 {
		r := codexStart(t, a, req)
		codexAdvance(t, a, server, 10*time.Millisecond)
		require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
		require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: r, HarvestAccepted: true, Outcome: "success"}))
	}
	other := *a
	other.rdb = redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = other.rdb.Close() })
	pool := req
	pool.PoolMode, pool.FixedProxy = true, nil
	pool.Selection.Restricted, pool.Selection.IDs = true, []int64{1, 2}
	r, err := other.ReserveCodexTicket(ctx, pool)
	require.NoError(t, err)
	require.EqualValues(t, 1, r.Proxy.ID, "新实例应复用已学到的成功率")
	codexFinish(t, &other, r, "canceled")
	r = codexStart(t, a, req)
	codexFinish(t, a, r, "canceled")
	r, err = other.ReserveCodexTicket(ctx, pool)
	require.NoError(t, err)
	require.EqualValues(t, 2, r.Proxy.ID, "每五次尝试给未知节点探索机会")
	codexFinish(t, &other, r, "canceled")
	codexAdvance(t, a, server, 31*24*time.Hour)
	require.Zero(t, a.rdb.Exists(ctx, codexLearningKey(7, "astra", "harvest", req.FixedProxy)).Val())
}

func TestCodexSchedulerLearningQualityFailureOverridesHarvestAndSmoothsLatency(t *testing.T) {
	a, server, req := newCodexPinTest(t, false)
	server.SetTime(time.Unix(1700000000, 0))
	req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
	ctx := context.Background()
	for i, delay := range []time.Duration{100, 300, 400} {
		r := codexStart(t, a, req)
		codexAdvance(t, a, server, delay*time.Millisecond)
		require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
		outcome := "success"
		if i == 2 {
			outcome = "verification_failed"
		}
		require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{
			Reservation: r, HarvestAccepted: true, QualityProxyFailed: i == 2, Outcome: outcome,
		}))
	}
	stats := codexLearningStats(t, a, 7, "astra", "harvest", codexPoolCandidate(1).proxy)
	require.Equal(t, float64(2), stats["successes"])
	require.Equal(t, float64(1), stats["failures"])
	require.Equal(t, float64(212), stats["latency_ms"])
	require.Equal(t, "quality_failed", stats["last_result"])
	require.Greater(t, stats["cooldown_until"].(float64), stats["updated_at"].(float64))
}

func TestCodexSchedulerLearningFencesChangedPolicyAndProxyVersion(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	req.PoolMode, req.FixedProxy = false, codexPoolCandidate(1).proxy
	settings := &codexSchedulerSettingsStub{cfg: req.Config}
	a.settings = settings
	ctx := context.Background()
	r := codexStart(t, a, req)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	settings.cfg.TTLSeconds++
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: r, HarvestAccepted: true, Outcome: "success"}))
	require.Zero(t, a.rdb.Exists(ctx, codexLearningKey(7, "astra", "harvest", req.FixedProxy)).Val())
	req.Config = settings.cfg
	r = codexStart(t, a, req)
	require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
	require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: r, HarvestAccepted: true, Outcome: "success"}))
	proxy := *req.FixedProxy
	proxy.Host = "replacement.invalid"
	require.Zero(t, a.rdb.Exists(ctx, codexLearningKey(7, "astra", "harvest", &proxy)).Val())
	require.Equal(t, float64(1), codexLearningStats(t, a, 7, "astra", "harvest", req.FixedProxy)["successes"])
}

func TestCodexSchedulerLearningUsesLatencyWithinSameSuccessRate(t *testing.T) {
	a, server, req := newCodexPinTest(t, false)
	server.SetTime(time.Unix(1700000000, 0))
	req.PoolMode = false
	ctx := context.Background()
	for i, delay := range []time.Duration{300, 100} {
		req.FixedProxy = codexPoolCandidate(int64(i + 1)).proxy
		r := codexStart(t, a, req)
		codexAdvance(t, a, server, delay*time.Millisecond)
		require.NoError(t, a.ReportCodexTicketHarvest(ctx, r, true, false, false))
		require.NoError(t, a.FinishCodexTicket(ctx, service.CodexTicketFinishRequest{Reservation: r, HarvestAccepted: true, Outcome: "success"}))
	}
	req.PoolMode, req.FixedProxy = true, nil
	req.Selection.Restricted, req.Selection.IDs = true, []int64{1, 2}
	r := codexStart(t, a, req)
	require.EqualValues(t, 2, r.Proxy.ID)
	codexFinish(t, a, r, "canceled")
}

func TestCodexSchedulerLearningCorruptRecordDoesNotBlockScheduling(t *testing.T) {
	a, _, req := newCodexPinTest(t, false)
	req.Selection.Restricted, req.Selection.IDs = true, []int64{1}
	key := codexLearningKey(7, "astra", "harvest", codexPoolCandidate(1).proxy)
	require.NoError(t, a.rdb.Set(context.Background(), key, "incomplete-json", time.Hour).Err())
	r := codexStart(t, a, req)
	require.EqualValues(t, 1, r.Proxy.ID)
	codexFinish(t, a, r, "canceled")
}
