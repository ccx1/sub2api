package service

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketQueuedAutomaticCancellationDoesNotWaitForManual(t *testing.T) {
	svc, upstream := codexTicketBoundedService(t, 2)
	manualCtx, cancelManual := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelManual()
	account := ticketTestAccount(1)
	manual := svc.submitCodexTicketProbes(withCodexTicketManualRetry(manualCtx), account, []string{"gpt-6-astra"}, true)
	awaitCodexTicketProbeSignals(t, upstream.entered, 1)
	autoCtx, cancelAutomatic := context.WithCancel(context.Background())
	defer cancelAutomatic()
	autoDone := make(chan struct{})
	go func() { svc.probeOnceOpenAICodexTicket(autoCtx, account, "gpt-6-astra"); close(autoDone) }()
	queue := svc.codexTicketAccountQueue(account.ID)
	require.Eventually(t, func() bool {
		queue.mu.Lock()
		defer queue.mu.Unlock()
		return len(queue.tasks) == 1
	}, time.Second, time.Millisecond)
	cancelAutomatic()
	requireCodexQueueDoneSoon(t, autoDone)
	require.EqualValues(t, 1, svc.openaiCodexTicketActive.Load())
	require.EqualValues(t, 1, svc.openaiCodexTicketManualPending.Load())
	upstream.permit <- struct{}{}
	awaitCodexTicketProbeSignals(t, manual[0].done, 1)
	require.Zero(t, svc.openaiCodexTicketActive.Load())
	require.Zero(t, svc.openaiCodexTicketManualPending.Load())
	after := svc.submitCodexTicketProbes(context.Background(), ticketTestAccount(2), []string{"gpt-6-astra"}, false)
	awaitCodexTicketProbeSignals(t, upstream.entered, 1)
	upstream.permit <- struct{}{}
	awaitCodexTicketProbeSignals(t, after[0].done, 1)
	require.EqualValues(t, 2, upstream.calls.Load())
	require.Zero(t, svc.openaiCodexTicketActive.Load())
}

type codexQueueControlsChangedRepo struct {
	*codexScheduleRepo
	manualEntered chan struct{}
	release       chan struct{}
	once          sync.Once
	manualCalls   atomic.Int32
}

func (r *codexQueueControlsChangedRepo) ReserveCodexTicket(ctx context.Context, in CodexTicketReserveRequest) (*CodexTicketReservation, error) {
	if !in.Manual {
		return r.codexScheduleRepo.ReserveCodexTicket(ctx, in)
	}
	r.manualCalls.Add(1)
	if r.manualEntered != nil {
		r.once.Do(func() { close(r.manualEntered) })
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-r.release:
		}
	}
	return nil, &CodexTicketWaitError{Status: &CodexTicketRuntimeStatus{State: "waiting", Reason: "controls_changed"}}
}

func TestCodexTicketManualControlsChangedReturnsWithoutRetryingStaleConfig(t *testing.T) {
	svc, base := codexScheduledFixture(t, true)
	repo := &codexQueueControlsChangedRepo{codexScheduleRepo: base}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	_, err := svc.reserveCodexTicketWithManualWait(ctx, repo, CodexTicketReserveRequest{AccountID: base.account.ID,
		Model: "gpt-6-astra", Config: svc.openAICodexTicketConfig(), Manual: true})
	var wait *CodexTicketWaitError
	require.ErrorAs(t, err, &wait)
	require.Equal(t, "controls_changed", wait.Status.Reason)
	require.NoError(t, ctx.Err())
	require.EqualValues(t, 1, repo.manualCalls.Load())
}

func TestCodexTicketManualControlsChangedReleasesAutomaticSuccessor(t *testing.T) {
	svc, base := codexScheduledFixture(t, true)
	repo := &codexQueueControlsChangedRepo{codexScheduleRepo: base, manualEntered: make(chan struct{}), release: make(chan struct{})}
	svc.accountRepo = repo
	var calls atomic.Int32
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(356)), nil
	}}
	manualCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	manual := svc.submitCodexTicketProbes(withCodexTicketManualRetry(manualCtx), base.account, []string{"gpt-6-astra"}, true)
	awaitCodexTicketProbeSignals(t, repo.manualEntered, 1)
	automatic := svc.submitCodexTicketProbes(context.Background(), base.account, []string{"gpt-6-astra"}, false)
	close(repo.release)
	requireCodexQueueDoneSoon(t, manual[0].done)
	awaitCodexTicketProbeSignals(t, automatic[0].done, 1)
	require.EqualValues(t, 1, repo.manualCalls.Load())
	require.EqualValues(t, 2, calls.Load())
	require.NotNil(t, svc.lookupOpenAICodexTicket(base.account, "gpt-6-astra"))
	require.Zero(t, svc.openaiCodexTicketActive.Load())
	require.Zero(t, svc.openaiCodexTicketManualPending.Load())
}

func requireCodexQueueDoneSoon(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(300 * time.Millisecond):
		t.Fatal("queue did not release canceled or unrecoverable work promptly")
	}
}
