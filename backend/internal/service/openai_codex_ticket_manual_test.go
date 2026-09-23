package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketManualBatchBudgetCoversAllQualityRounds(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, BusinessVerificationRounds: 10, HarvestAttemptTimeoutSeconds: 25,
	}, nil)
	account := ticketTestAccount(41)
	queue := svc.codexTicketAccountQueue(account.ID)
	queue.running = true
	started := time.Now()
	svc.submitManualCodexTicketProbes(context.Background(), account, []string{"gpt-6-astra", "gpt-5.6-sol"})
	queue.mu.Lock()
	tasks := append([]*codexTicketProbeTask(nil), queue.tasks...)
	queue.mu.Unlock()
	t.Cleanup(func() {
		for _, task := range tasks {
			queue.cancelPending(task)
		}
	})
	require.Len(t, tasks, 2)
	for _, task := range tasks {
		deadline, ok := task.ctx.Deadline()
		require.True(t, ok)
		// 每模型一次采集、十轮质量验证和独立业务出口验证均可用满单次超时。
		minimum := 5*time.Minute + 2*12*25*time.Second
		require.GreaterOrEqual(t, deadline.Sub(started), minimum)
	}
}

func TestCodexTicketManualBatchOutlivesAdminRequest(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, nil)
	account := ticketTestAccount(41)
	queue := svc.codexTicketAccountQueue(account.ID)
	queue.running = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc.submitManualCodexTicketProbes(ctx, account, []string{"gpt-6-astra"})
	queue.mu.Lock()
	task := queue.tasks[0]
	queue.mu.Unlock()
	t.Cleanup(func() { queue.cancelPending(task) })
	require.NoError(t, task.ctx.Err())
	require.True(t, isCodexTicketManualRetry(task.ctx))
	require.True(t, queue.cancelPending(task))
	require.Eventually(t, func() bool { return task.ctx.Err() != nil }, time.Second, time.Millisecond)
	require.Zero(t, svc.openaiCodexTicketManualPending.Load())
}

func TestCodexTicketManualBatchCancellationRemovesQueuedPriority(t *testing.T) {
	svc, upstream := codexTicketBoundedService(t, 2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	account := ticketTestAccount(1)
	active := svc.submitCodexTicketProbes(ctx, account, []string{"gpt-6-astra"}, false)
	awaitCodexTicketProbeSignals(t, upstream.entered, 1)
	manualCtx, cancelManual := context.WithCancel(withCodexTicketManualRetry(ctx))
	defer cancelManual()
	manual := svc.submitCodexTicketProbes(manualCtx, account, []string{"gpt-6-astra", "gpt-5.6-sol"}, true)
	cleaned := make(chan struct{})
	go func() {
		waitManualCodexTicketBatch(manualCtx, svc.codexTicketAccountQueue(account.ID), manual)
		close(cleaned)
	}()
	cancelManual()
	requireCodexQueueDoneSoon(t, cleaned)
	for _, task := range manual {
		requireCodexQueueDoneSoon(t, task.done)
	}
	require.Zero(t, svc.openaiCodexTicketManualPending.Load())
	require.EqualValues(t, 1, svc.openaiCodexTicketActive.Load())
	other := svc.submitCodexTicketProbes(ctx, ticketTestAccount(2), []string{"gpt-6-astra"}, false)
	awaitCodexTicketProbeSignals(t, upstream.entered, 1)
	upstream.permit <- struct{}{}
	upstream.permit <- struct{}{}
	awaitCodexTicketProbeSignals(t, active[0].done, 1)
	awaitCodexTicketProbeSignals(t, other[0].done, 1)
	require.Zero(t, svc.openaiCodexTicketActive.Load())
	require.EqualValues(t, 2, upstream.calls.Load())
}

func TestCodexTicketManualBatchForcesFreshMintDuringAutomaticCooldown(t *testing.T) {
	svc, repo := codexScheduledFixture(t, true)
	svc.cfg.Gateway.OpenAICodexTicket.PoolCapacity = 1
	account := repo.account
	account.Extra[DailyCooldownExtraKey] = codexTicketControlDailyCooldown()
	old := &openAICodexTicket{AccountID: account.ID, Model: "gpt-6-astra", State: fakeCodexTicketState(292),
		Length: 292, Verified: true, CapturedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour),
		Binding: svc.codexTicketBinding(account)}
	require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, old))
	svc.advanceCodexTicketBackoff(&openAICodexTicketProbeInput{Account: account, Token: "tok", Model: old.Model,
		Attempt: &CodexTicketAttempt{Reason: "ticket_not_matched"}}, time.Now())
	svc.coolOpenAICodexTicket(account, "tok", &openAICodexTicketProbeRejected{Status: http.StatusTooManyRequests})
	upstream := &codexTicketVerificationUpstream{respond: func(int) *http.Response {
		return codexTicketCompletedResponse(old.Model, "gAAAAA"+strings.Repeat("C", 286))
	}}
	svc.httpUpstream = upstream
	svc.probeOnceOpenAICodexTicket(context.Background(), account, old.Model)
	require.Empty(t, upstream.requests)
	svc.retryScheduledCodexTickets(context.Background(), account, []string{old.Model}, svc.openAICodexTicketConfig())
	require.Eventually(t, func() bool { return svc.openaiCodexTicketManualPending.Load() == 0 }, time.Second, time.Millisecond)
	require.Len(t, upstream.requests, 2)
	require.Empty(t, upstream.requests[0].Header.Get(openAICodexTurnStateHeader), "手动入口必须重新采集，不能只复验旧票")
	require.NotEqual(t, old.State, svc.lookupOpenAICodexTicket(account, old.Model).State)
	require.Len(t, repo.history, 1)
	require.True(t, repo.history[0].Success)
}

func TestCodexTicketManualBatchCompletesTenRoundsForEachModel(t *testing.T) {
	svc, repo := codexQualityFixture(t, config.CodexTicketCredentialCookieState)
	models := []string{"gpt-6-astra", "gpt-5.6-sol"}
	svc.cfg.Gateway.OpenAICodexTicket.Models = models
	svc.cfg.Gateway.OpenAICodexTicket.BusinessVerificationRounds = 10
	business := &Proxy{ID: 9, Status: StatusActive, Protocol: "http", Host: "business.example", Port: 8080}
	repo.account.ProxyID, repo.account.Proxy = &business.ID, business
	calls, mints := map[string]int{}, map[string]int{}
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		model := strings.TrimPrefix(req.Header.Get(openAICodexRoutingHintHeader), "model=")
		calls[model]++
		if req.Header.Get(openAICodexTurnStateHeader) == "" {
			mints[model]++
		}
		return codexQualityResponse(calls[model], model), nil
	}}
	result := svc.retryScheduledCodexTickets(context.Background(), repo.account, models, svc.openAICodexTicketConfig())
	require.Equal(t, 2, result.Scheduled)
	require.Eventually(t, func() bool { return svc.openaiCodexTicketManualPending.Load() == 0 }, time.Second, time.Millisecond)
	for _, model := range models {
		require.Equal(t, 12, calls[model])
		require.Equal(t, 1, mints[model])
		require.NotNil(t, svc.lookupOpenAICodexTicket(repo.account, model))
	}
	require.Len(t, repo.history, 2)
	for _, attempt := range repo.history {
		require.True(t, attempt.Success)
		require.Equal(t, 10, attempt.BusinessVerificationPassed)
	}
}
