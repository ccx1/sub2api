package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketSharedRejectionDoesNotStackLocalBackoff(t *testing.T) {
	svc, repo := codexScheduledFixture(t, false)
	svc.cfg.Gateway.OpenAICodexTicket.LengthMode = config.CodexTicketLengthStrict
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312)), nil
	}}
	key := codexTicketBackoffKey(repo.account, "tok", "gpt-5.6-sol")
	svc.openaiCodexTicketBackoff.Store(key, &codexTicketBackoffState{Failures: 5, UpdatedAt: time.Now(), RetryAt: time.Now().Add(time.Hour)})
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Len(t, repo.finishes, 1)
	require.Equal(t, "ticket_rejected", repo.finishes[0].Outcome)
	require.False(t, svc.openAICodexTicketBackoffActive(repo.account, "tok", "gpt-6-astra"))
	require.False(t, svc.openAICodexTicketBackoffActive(repo.account, "tok", "gpt-5.6-sol"))
}

func TestCodexTicketSuccessClearsAccountBackoffWithoutAffectingOtherAccounts(t *testing.T) {
	svc, repo := codexScheduledFixture(t, false)
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(292)), nil
	}}
	other := ticketTestAccount(repo.account.ID + 1)
	for _, account := range []*Account{repo.account, other} {
		key := codexTicketBackoffKey(account, "tok", "gpt-5.6-sol")
		svc.openaiCodexTicketBackoff.Store(key, &codexTicketBackoffState{Failures: 5, UpdatedAt: time.Now(), RetryAt: time.Now().Add(time.Hour)})
	}
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Len(t, repo.finishes, 1)
	require.Equal(t, "success", repo.finishes[0].Outcome)
	require.False(t, svc.openAICodexTicketBackoffActive(repo.account, "tok", "gpt-5.6-sol"))
	require.True(t, svc.openAICodexTicketBackoffActive(other, "tok", "gpt-5.6-sol"))
}
