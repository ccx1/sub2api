package repository

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func codexTransportMember(proxy *service.Proxy) string {
	return fmt.Sprintf("%d:%x", proxy.ID, sha256.Sum256([]byte(proxy.URL())))
}

func coolCodexTransport(t *testing.T, a *ProxyPoolAllocator, proxy *service.Proxy, delay time.Duration) {
	t.Helper()
	ctx := context.Background()
	now, err := a.rdb.Time(ctx).Result()
	require.NoError(t, err)
	require.NoError(t, a.rdb.ZAdd(ctx, "proxy:{pool}:transport_cooldowns", redis.Z{
		Member: codexTransportMember(proxy), Score: float64(now.Add(delay).UnixMilli()),
	}).Err())
}

func TestCodexSchedulerTransportCooldownPrefersAvailableRegardlessOfProtection(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		for _, manual := range []bool{true, false} {
			t.Run(fmt.Sprintf("enabled_%t_manual_%t", enabled, manual), func(t *testing.T) {
				a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
				cfg := codexSchedulerConfig()
				cfg.Protection.Enabled = enabled
				req := codexRequest(7, cfg)
				req.Manual = manual
				coolCodexTransport(t, a, codexPoolCandidate(1).proxy, 5*time.Minute)
				r := codexStart(t, a, req)
				require.EqualValues(t, 2, r.Proxy.ID)
				require.False(t, r.HalfOpen)
				require.EqualValues(t, 1, a.rdb.ZCard(context.Background(), "proxy:{pool}:transport_cooldowns").Val())
				codexFinish(t, a, r, "success")
				req.PoolMode, req.FixedProxy = false, codexPoolCandidate(1).proxy
				_, err := a.ReserveCodexTicket(context.Background(), req)
				requireCodexWait(t, err, "proxy_silent")
			})
		}
	}
}

func TestCodexSchedulerTransportCooldownRecoversOldestOneAtATime(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		t.Run(fmt.Sprintf("enabled_%t", enabled), func(t *testing.T) {
			a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
			ctx, cfg := context.Background(), codexSchedulerConfig()
			cfg.Protection.Enabled = enabled
			coolCodexTransport(t, a, codexPoolCandidate(1).proxy, 5*time.Minute)
			coolCodexTransport(t, a, codexPoolCandidate(2).proxy, 4*time.Minute)
			req := codexRequest(7, cfg)
			req.Manual, req.HarvestUsesBusiness, req.AllowDirectOnEmpty = true, true, true
			first := codexStart(t, a, req)
			require.EqualValues(t, 2, first.Proxy.ID)
			require.True(t, first.HalfOpen)
			require.EqualValues(t, 1, a.rdb.ZCard(ctx, "proxy:{pool}:transport_cooldowns").Val())
			_, err := a.rdb.ZScore(ctx, "proxy:{pool}:transport_cooldowns", codexTransportMember(first.Proxy)).Result()
			require.ErrorIs(t, err, redis.Nil)
			other := codexRequest(8, cfg)
			_, err = a.ReserveCodexTicket(ctx, other)
			requireCodexWait(t, err, "half_open_busy")
			codexAdvance(t, a, server, time.Second)
			coolCodexTransport(t, a, first.Proxy, 5*time.Minute)
			codexFinish(t, a, first, "upstream_error")
			second := codexStart(t, a, other)
			require.EqualValues(t, 1, second.Proxy.ID)
			require.True(t, second.HalfOpen)
			require.NoError(t, a.ReportCodexTicketHarvest(ctx, second, true, false, false))
			codexFinish(t, a, second, "success")
			next := codexStart(t, a, codexRequest(9, cfg))
			require.EqualValues(t, 1, next.Proxy.ID)
			require.False(t, next.HalfOpen)
			codexFinish(t, a, next, "success")
		})
	}
}

func TestCodexSchedulerTransportCooldownRechecksStartAndStage(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
	ctx, cfg := context.Background(), codexSchedulerConfig()
	cfg.Protection.Enabled = false
	req := codexRequest(7, cfg)
	req.PoolMode, req.FixedProxy, req.Manual = false, codexPoolCandidate(1).proxy, true
	r, err := a.ReserveCodexTicket(ctx, req)
	require.NoError(t, err)
	coolCodexTransport(t, a, r.Proxy, 5*time.Minute)
	requireCodexWait(t, a.StartCodexTicket(ctx, r), "proxy_silent")
	codexFinish(t, a, r, "canceled")
	req.FixedProxy = codexPoolCandidate(2).proxy
	r = codexStart(t, a, req)
	requireCodexWait(t, a.CheckCodexTicketStage(ctx, r, codexPoolCandidate(1).proxy), "proxy_silent")
	codexFinish(t, a, r, "canceled")
}

func TestCodexSchedulerTransportCooldownRespectsCapacityScopeAndEndpoint(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, codexPoolCandidate(1, "99"), codexPoolCandidate(2), codexPoolCandidate(3))
	ctx := context.Background()
	coolCodexTransport(t, a, codexPoolCandidate(1).proxy, time.Minute)
	coolCodexTransport(t, a, codexPoolCandidate(2).proxy, 2*time.Minute)
	req := codexRequest(7, codexSchedulerConfig())
	req.Selection.Restricted, req.Selection.IDs = true, []int64{1, 2}
	r := codexStart(t, a, req)
	require.EqualValues(t, 2, r.Proxy.ID)
	require.True(t, r.HalfOpen)
	_, err := a.rdb.ZScore(ctx, "proxy:{pool}:transport_cooldowns", codexTransportMember(codexPoolCandidate(1).proxy)).Result()
	require.NoError(t, err)
	codexFinish(t, a, r, "canceled")
	changed := codexPoolCandidate(1).proxy
	changed.Host = "replacement.invalid"
	req.PoolMode, req.FixedProxy = false, changed
	r = codexStart(t, a, req)
	require.False(t, r.HalfOpen, "旧出口冷却不能影响相同ID的新地址")
	codexFinish(t, a, r, "success")
}

func TestCodexSchedulerTransportCooldownRecoveryIsExclusive(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1), codexPoolCandidate(2))
	coolCodexTransport(t, a, codexPoolCandidate(1).proxy, 4*time.Minute)
	coolCodexTransport(t, a, codexPoolCandidate(2).proxy, 5*time.Minute)
	results := make(chan *service.CodexTicketReservation, 16)
	errors := make(chan error, 16)
	var wait sync.WaitGroup
	for id := int64(1); id <= 16; id++ {
		wait.Go(func() {
			r, err := a.ReserveCodexTicket(context.Background(), codexRequest(id, codexSchedulerConfig()))
			if err != nil {
				errors <- err
			} else {
				results <- r
			}
		})
	}
	wait.Wait()
	close(results)
	close(errors)
	require.Len(t, results, 1)
	require.Len(t, errors, 15)
	for err := range errors {
		requireCodexWait(t, err, "half_open_busy")
	}
	for result := range results {
		require.EqualValues(t, 1, result.Proxy.ID)
		require.True(t, result.HalfOpen)
		codexFinish(t, a, result, "canceled")
	}
}

func TestCodexSchedulerTransportCooldownCanceledRecoveryRestoresOriginalDeadline(t *testing.T) {
	for _, outcome := range []string{"canceled", "expired", "new_failure"} {
		t.Run(outcome, func(t *testing.T) {
			a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
			ctx := context.Background()
			proxy := codexPoolCandidate(1).proxy
			coolCodexTransport(t, a, proxy, 5*time.Minute)
			member := codexTransportMember(proxy)
			expected, err := a.rdb.ZScore(ctx, "proxy:{pool}:transport_cooldowns", member).Result()
			require.NoError(t, err)
			req := codexRequest(7, codexSchedulerConfig())
			r, err := a.ReserveCodexTicket(ctx, req)
			require.NoError(t, err)
			require.True(t, r.HalfOpen)
			if outcome == "new_failure" {
				codexAdvance(t, a, server, time.Second)
				coolCodexTransport(t, a, proxy, 5*time.Minute)
				expected, err = a.rdb.ZScore(ctx, "proxy:{pool}:transport_cooldowns", member).Result()
				require.NoError(t, err)
			}
			if outcome == "expired" {
				codexAdvance(t, a, server, time.Minute)
				req.PoolMode, req.FixedProxy = false, proxy
				_, err = a.ReserveCodexTicket(ctx, req)
				requireCodexWait(t, err, "proxy_silent")
			} else {
				codexFinish(t, a, r, "canceled")
			}
			restored, err := a.rdb.ZScore(ctx, "proxy:{pool}:transport_cooldowns", member).Result()
			require.NoError(t, err)
			require.Equal(t, expected, restored, "取消或过期不丢失原冷却，也不能覆盖新故障期限")
			require.Positive(t, a.rdb.PTTL(ctx, "proxy:{pool}:transport_cooldowns").Val())
		})
	}
}
