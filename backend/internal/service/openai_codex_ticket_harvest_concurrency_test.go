package service

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexTicketBoundedUpstream struct {
	HTTPUpstream
	entered                       chan struct{}
	permit                        chan struct{}
	active, peak, calls, canceled atomic.Int64
}

func (u *codexTicketBoundedUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	active := u.active.Add(1)
	defer u.active.Add(-1)
	u.calls.Add(1)
	for peak := u.peak.Load(); active > peak; peak = u.peak.Load() {
		if u.peak.CompareAndSwap(peak, active) {
			break
		}
	}
	u.entered <- struct{}{}
	select {
	case <-req.Context().Done():
		u.canceled.Add(1)
		return nil, req.Context().Err()
	case <-u.permit:
		// 完整响应返回无效候选长度，避免业务复验干扰采集并发计数。
		return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312)), nil
	}
}

func codexTicketBoundedService(t *testing.T, count int) (*OpenAIGatewayService, *codexTicketBoundedUpstream) {
	t.Helper()
	u := &codexTicketBoundedUpstream{entered: make(chan struct{}, count), permit: make(chan struct{}, count)}
	repo := &codexTicketRefreshRepo{}
	for i := 0; i < count; i++ {
		account := ticketTestAccount(int64(i + 1))
		account.Status = StatusActive
		repo.accounts = append(repo.accounts, *account)
	}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true,
		Models: []string{"gpt-6-astra"}, HarvestProxyURL: "http://harvest.example:8080",
		HarvestAttemptTimeoutSeconds: 60}, u)
	svc.accountRepo = repo
	return svc, u
}

func awaitCodexTicketProbeSignals(t *testing.T, events <-chan struct{}, count int) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for i := 0; i < count; i++ {
		select {
		case <-events:
		case <-timer.C:
			t.Fatalf("received %d of %d expected probe signals", i, count)
		}
	}
}

func TestCodexTicketHarvestConcurrencyIsBoundedAcrossAccounts(t *testing.T) {
	const accounts, concurrency = 24, 8
	svc, upstream := codexTicketBoundedService(t, accounts)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { svc.refreshOpenAICodexTickets(ctx); close(done) }()
	for batch := 0; batch < accounts/concurrency; batch++ {
		awaitCodexTicketProbeSignals(t, upstream.entered, concurrency)
		require.LessOrEqual(t, upstream.active.Load(), int64(concurrency))
		require.LessOrEqual(t, upstream.peak.Load(), int64(concurrency))
		for i := 0; i < concurrency; i++ {
			upstream.permit <- struct{}{}
		}
	}
	awaitCodexTicketProbeSignals(t, done, 1)
	require.Equal(t, int64(accounts), upstream.calls.Load())
	require.Equal(t, int64(concurrency), upstream.peak.Load())
	require.Zero(t, upstream.active.Load())
	require.Zero(t, upstream.canceled.Load())
}

func TestCodexTicketHarvestConcurrencyCancellationStopsQueuedAccounts(t *testing.T) {
	svc, upstream := codexTicketBoundedService(t, 24)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { svc.refreshOpenAICodexTickets(ctx); close(done) }()
	awaitCodexTicketProbeSignals(t, upstream.entered, 8)
	cancel()
	awaitCodexTicketProbeSignals(t, done, 1)
	require.Equal(t, int64(8), upstream.calls.Load(), "排队账号取消后不得继续出站")
	require.Equal(t, int64(8), upstream.canceled.Load())
	require.Zero(t, upstream.active.Load())
	// 再次调用同一已取消上下文也不得产生下一轮采集。
	svc.refreshOpenAICodexTickets(ctx)
	require.Equal(t, int64(8), upstream.calls.Load())
}
