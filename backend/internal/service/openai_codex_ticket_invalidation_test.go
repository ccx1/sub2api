package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func captureTicketInvalidations(s *OpenAIGatewayService) <-chan *openAICodexTicket {
	writes := make(chan *openAICodexTicket, 4)
	s.accountRepo = &codexTicketCASStub{update: func(_ *Account, _ string, value any) (bool, error) {
		writes <- value.(*openAICodexTicket)
		return true, nil
	}}
	return writes
}

func awaitTicketInvalidation(t *testing.T, writes <-chan *openAICodexTicket) *openAICodexTicket {
	t.Helper()
	select {
	case ticket := <-writes:
		require.True(t, ticket.Revoked)
		require.NotNil(t, ticket.Invalidation)
		return ticket
	case <-time.After(2 * time.Second):
		t.Fatal("ticket invalidation was not persisted")
		return nil
	}
}

func TestCodexTicketInvalidationHTTPRecordsTriggerAndPreservesResponse(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		s, _, ticket, request := ticketWatchdogFixture(t)
		receipt := request.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
		receipt.ticket.AttemptID = "attempt-http"
		writes := captureTicketInvalidations(s)
		state, reason := "", "response_model_mismatch"
		if rejected {
			state, reason = fakeCodexTicketState(312), "response_ticket_rejected"
		}
		response := codexTicketCompletedResponse("gpt-other", state)
		s.observeOpenAICodexTicketResponse(request, response)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.Contains(t, string(body), "gpt-other")
		stored := awaitTicketInvalidation(t, writes)
		event := stored.Invalidation
		require.Equal(t, "attempt-http", event.AttemptID)
		require.Equal(t, "http", event.Source)
		require.Equal(t, reason, event.Reason)
		require.Equal(t, ticket.Model, event.Model)
		require.Equal(t, ticket.CapturedAt, event.CapturedAt)
		require.WithinDuration(t, time.Now(), event.InvalidatedAt, 5*time.Second)
		if rejected {
			require.Equal(t, 312, *event.ReturnedTicketLength)
		} else {
			require.Equal(t, []string{"gpt-other"}, event.ReportedModels)
		}
		encoded, err := json.Marshal(event)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), ticket.State)
		require.NotContains(t, string(encoded), "original body")
		require.Empty(t, writes, "one response must not record a second cause")
	}
}

func TestCodexTicketInvalidationWSRecordsModelAndHandshakeCauses(t *testing.T) {
	for _, handshake := range []bool{false, true} {
		s, _, receipt := codexTicketWSFixture(t)
		receipt.ticket.AttemptID = "attempt-ws"
		writes := captureTicketInvalidations(s)
		if handshake {
			receipt.observeHandshake(context.Background(), s, http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{fakeCodexTicketState(312)}})
		} else {
			watch := receipt.watch(context.Background(), s, receipt.ticket.Model)
			watch.observe([]byte(`{"type":"response.completed","response":{"status":"completed","model":"gpt-other"}}`))
		}
		event := awaitTicketInvalidation(t, writes).Invalidation
		require.Equal(t, "attempt-ws", event.AttemptID)
		if handshake {
			require.Equal(t, "websocket_handshake", event.Source)
			require.Equal(t, "response_ticket_rejected", event.Reason)
			require.Equal(t, 312, *event.ReturnedTicketLength)
		} else {
			require.Equal(t, "websocket", event.Source)
			require.Equal(t, "response_model_mismatch", event.Reason)
			require.Equal(t, []string{"gpt-other"}, event.ReportedModels)
		}
	}
}

func TestCodexTicketInvalidationRetryKeepsOriginalObservation(t *testing.T) {
	s, account, ticket, _ := ticketWatchdogFixture(t)
	ticket.AttemptID = "attempt-retry"
	event := newCodexTicketInvalidation("response_model_mismatch", "http", []string{"gpt-other"})
	var attempts []*CodexTicketInvalidation
	s.accountRepo = &codexTicketCASStub{update: func(_ *Account, _ string, value any) (bool, error) {
		attempts = append(attempts, value.(*openAICodexTicket).Invalidation)
		if len(attempts) == 1 {
			return false, errors.New("transient write failure")
		}
		return true, nil
	}}
	s.invalidateOpenAICodexTicket(context.Background(), account, ticket, event)
	require.Len(t, attempts, 2)
	require.Equal(t, attempts[0], attempts[1])
	require.Equal(t, event.InvalidatedAt, attempts[1].InvalidatedAt)
	require.Nil(t, ticket.Invalidation, "do not mutate the sent snapshot")
	require.Empty(t, event.AttemptID, "do not mutate caller-owned observation")
}

func TestCodexTicketInvalidationHarvestLinksPublishedTicketAndHistory(t *testing.T) {
	upstream := &codexTicketVerificationUpstream{respond: func(int) *http.Response {
		return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(292))
	}}
	s := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://harvest.example:8080"}, upstream)
	repo := &codexTicketHistoryRepo{}
	s.accountRepo = repo
	account := ticketTestAccount(41)
	s.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Len(t, repo.history.Items, 1)
	attempt := repo.history.Items[0]
	ticket := s.lookupOpenAICodexTicket(account, attempt.Model)
	require.NotNil(t, ticket)
	require.True(t, attempt.Success)
	require.Equal(t, attempt.ID, ticket.AttemptID)
	require.Equal(t, ticket.CapturedAt, *attempt.TicketCapturedAt)
	require.Equal(t, ticket.ExpiresAt, *attempt.TicketExpiresAt)
}
