package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexQueueEvent struct {
	manual bool
	model  string
}

func TestCodexTicketQueueManualPreemptsAutomaticAndResumesAfterFailure(t *testing.T) {
	events := make(chan codexQueueEvent, 8)
	permit := make(chan struct{}, 8)
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		events <- codexQueueEvent{isCodexTicketManualRetry(req.Context()), req.Header.Get(openAICodexRoutingHintHeader)}
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-permit:
			return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312)), nil
		}
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true,
		Models: []string{"gpt-6-astra", "gpt-5.6-sol"}, HarvestProxyURL: "http://harvest.example:8080"}, upstream)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	account := ticketTestAccount(41)
	first := svc.submitCodexTicketProbes(ctx, account, []string{"gpt-6-astra"}, false)
	require.Equal(t, codexQueueEvent{false, "model=gpt-6-astra"}, awaitCodexQueueEvent(t, events))
	auto := svc.submitCodexTicketProbes(ctx, account, []string{"gpt-5.6-sol"}, false)
	manual := svc.submitCodexTicketProbes(withCodexTicketManualRetry(ctx), account, []string{"gpt-6-astra", "gpt-6-astra"}, true)
	for i := 0; i < 2; i++ {
		permit <- struct{}{}
		require.Equal(t, codexQueueEvent{true, "model=gpt-6-astra"}, awaitCodexQueueEvent(t, events))
	}
	permit <- struct{}{}
	require.Equal(t, codexQueueEvent{false, "model=gpt-5.6-sol"}, awaitCodexQueueEvent(t, events))
	permit <- struct{}{}
	for _, task := range append(append(first, manual...), auto...) {
		awaitCodexTicketProbeSignals(t, task.done, 1)
	}
	require.Zero(t, svc.openaiCodexTicketManualPending.Load())
	require.Zero(t, svc.openaiCodexTicketActive.Load())
}

func awaitCodexQueueEvent(t *testing.T, events <-chan codexQueueEvent) codexQueueEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(3 * time.Second):
		t.Fatal("queued ticket request did not reach upstream")
		return codexQueueEvent{}
	}
}

func TestCodexTicketQueueCanceledManualReleasesAutomatic(t *testing.T) {
	svc, upstream := codexTicketBoundedService(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	manual := svc.submitCodexTicketProbes(withCodexTicketManualRetry(ctx), ticketTestAccount(1), []string{"gpt-6-astra"}, true)
	awaitCodexTicketProbeSignals(t, manual[0].done, 1)
	require.Zero(t, svc.openaiCodexTicketManualPending.Load())
	auto := svc.submitCodexTicketProbes(context.Background(), ticketTestAccount(1), []string{"gpt-6-astra"}, false)
	awaitCodexTicketProbeSignals(t, upstream.entered, 1)
	upstream.permit <- struct{}{}
	awaitCodexTicketProbeSignals(t, auto[0].done, 1)
	require.EqualValues(t, 1, upstream.calls.Load())
}

func TestCodexTicketHarvestIntervalHonorsShortRejectionPolicy(t *testing.T) {
	protection := config.DefaultCodexTicketProtection()
	protection.Enabled = true
	protection.RejectionRetryIntervalSeconds = 3
	protection.RejectionRetryCooldownSeconds = 15
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{HarvestProbeIntervalSeconds: 60, Protection: &protection}, nil)
	require.Equal(t, 3*time.Second, svc.codexTicketHarvestInterval(context.Background()))
	protection.Enabled = false
	require.Equal(t, 3*time.Second, svc.codexTicketHarvestInterval(context.Background()))
}
