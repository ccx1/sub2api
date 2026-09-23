package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketBusinessHoldSharedConcurrencyAndExpiry(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	otherRedis := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() { _ = otherRedis.Close() })
	other := &ProxyPoolAllocator{rdb: otherRedis, latencyCache: NewProxyLatencyCache(otherRedis), settings: a.settings, loadCandidates: a.loadCandidates}
	ctx := context.Background()
	first := service.CodexTicketBusinessLease{AccountID: 7, Token: "first", TTL: time.Minute}
	second := service.CodexTicketBusinessLease{AccountID: 7, Token: "second", TTL: time.Minute}
	require.NoError(t, a.AcquireCodexTicketBusinessHold(ctx, first))
	require.NoError(t, other.AcquireCodexTicketBusinessHold(ctx, second))
	_, err := other.ReserveCodexTicket(ctx, codexRequest(7, codexSchedulerConfig()))
	requireCodexWait(t, err, "business_active")
	manual := codexRequest(7, codexSchedulerConfig())
	manual.Manual = true
	_, err = other.ReserveCodexTicket(ctx, manual)
	requireCodexWait(t, err, "business_active")
	require.NoError(t, a.ReleaseCodexTicketBusinessHold(ctx, first))
	require.NoError(t, a.ReleaseCodexTicketBusinessHold(ctx, first))
	_, err = other.ReserveCodexTicket(ctx, manual)
	requireCodexWait(t, err, "business_active")
	status, err := other.GetCodexTicketRuntimeStatus(ctx, 7)
	require.NoError(t, err)
	require.Equal(t, "business_active", status.Reason)
	codexAdvance(t, a, server, 40*time.Second)
	require.NoError(t, other.RenewCodexTicketBusinessHold(ctx, second))
	codexAdvance(t, a, server, 40*time.Second)
	_, err = other.ReserveCodexTicket(ctx, manual)
	requireCodexWait(t, err, "business_active")
	codexAdvance(t, a, server, 61*time.Second)
	require.ErrorIs(t, other.RenewCodexTicketBusinessHold(ctx, second), service.ErrCodexTicketBusinessHoldLost)
	r, err := other.ReserveCodexTicket(ctx, manual)
	require.NoError(t, err)
	codexFinish(t, other, r, "canceled")
}

func TestCodexTicketBusinessHoldAllowsAdmittedHarvestToFinish(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, codexPoolCandidate(1))
	ctx := context.Background()
	r := codexStart(t, a, codexRequest(7, codexSchedulerConfig()))
	lease := service.CodexTicketBusinessLease{AccountID: 7, Token: "business", TTL: time.Minute}
	require.NoError(t, a.AcquireCodexTicketBusinessHold(ctx, lease))
	require.NoError(t, a.CheckCodexTicketStage(ctx, r, r.Proxy))
	codexFinish(t, a, r, "success")
	_, err := a.ReserveCodexTicket(ctx, codexRequest(7, codexSchedulerConfig()))
	requireCodexWait(t, err, "business_active")
	require.NoError(t, a.ReleaseCodexTicketBusinessHold(ctx, lease))
	status, err := a.GetCodexTicketRuntimeStatus(ctx, 7)
	require.NoError(t, err)
	require.NotEqual(t, "business_active", status.Reason)
	next := codexRequest(7, codexSchedulerConfig())
	next.Manual = true
	r, err = a.ReserveCodexTicket(ctx, next)
	require.NoError(t, err, "last release resumes admission without waiting for the lease deadline")
	codexFinish(t, a, r, "canceled")
}

func TestCodexTicketBusinessHoldMissingRedisFailsClosed(t *testing.T) {
	repo := &accountRepository{}
	lease := service.CodexTicketBusinessLease{AccountID: 7, Token: "business", TTL: time.Minute}
	require.Error(t, repo.AcquireCodexTicketBusinessHold(context.Background(), lease))
	_, err := repo.CodexTicketBusinessHoldUntil(context.Background(), 7)
	require.Error(t, err)
}
