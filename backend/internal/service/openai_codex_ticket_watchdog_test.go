package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func ticketWatchdogFixture(t *testing.T) (*OpenAIGatewayService, *Account, *openAICodexTicket, *http.Request) {
	t.Helper()
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	account := ticketTestAccount(41)
	ticket := &openAICodexTicket{Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292,
		CapturedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
	require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, ticket))
	req, err := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader("original body"))
	require.NoError(t, err)
	require.NoError(t, svc.applyOpenAICodexTicketRequest(account, ticket.Model, req))
	return svc, account, ticket, req
}

func TestCodexTicketWatchdogPreservesResponseAndRevokesMismatch(t *testing.T) {
	svc, account, _, req := ticketWatchdogFixture(t)
	response := codexTicketCompletedResponse("gpt-other", "")
	expected, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	response.Body = io.NopCloser(strings.NewReader(string(expected)))
	svc.observeOpenAICodexTicketResponse(req, response)
	actual, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, expected, actual)
	require.Equal(t, http.StatusOK, response.StatusCode)
	requestBody, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Equal(t, "original body", string(requestBody))
	require.Eventually(t, func() bool { return svc.lookupOpenAICodexTicket(account, "gpt-6-astra") == nil }, time.Second, time.Millisecond)
}

func TestCodexTicketWatchdogOldResponseCannotRevokeRenewal(t *testing.T) {
	svc, account, previous, req := ticketWatchdogFixture(t)
	next := *previous
	next.CapturedAt = time.Now()
	next.State = "gAAAAA" + strings.Repeat("C", 286)
	require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, &next))
	response := codexTicketCompletedResponse("gpt-other", "")
	svc.observeOpenAICodexTicketResponse(req, response)
	_, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	// 直接调用同一迟到撤销以确定等待回调执行，不依赖 sleep 判定没有发生的事。
	svc.invalidateOpenAICodexTicket(context.Background(), account, previous)
	require.Equal(t, next.State, svc.lookupOpenAICodexTicket(account, next.Model).State)
}

func TestCodexTicketWatchdogMissingHeaderOrFailedResponseKeepsTicket(t *testing.T) {
	for _, kind := range []string{"matching_without_state", "http_error", "failed_then_completed", "no_receipt"} {
		t.Run(kind, func(t *testing.T) {
			svc, account, ticket, req := ticketWatchdogFixture(t)
			response := codexTicketCompletedResponse(ticket.Model, "")
			switch kind {
			case "http_error":
				response.StatusCode = http.StatusTooManyRequests
			case "failed_then_completed":
				response.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-other\"}}\n\n"))
			case "no_receipt":
				req = req.WithContext(context.Background())
				response = codexTicketCompletedResponse("gpt-other", fakeCodexTicketState(312))
			}
			svc.observeOpenAICodexTicketResponse(req, response)
			_, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, ticket.State, svc.lookupOpenAICodexTicket(account, ticket.Model).State)
		})
	}
}

func TestCodexTicketWatchdogRejects312WithoutRewritingBody(t *testing.T) {
	svc, account, ticket, req := ticketWatchdogFixture(t)
	response := codexTicketCompletedResponse(ticket.Model, fakeCodexTicketState(312))
	svc.observeOpenAICodexTicketResponse(req, response)
	_, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Eventually(t, func() bool { return svc.lookupOpenAICodexTicket(account, ticket.Model) == nil }, time.Second, time.Millisecond)
}

func TestCodexTicketRevocationCannotBeResurrectedByOldExtra(t *testing.T) {
	svc, account, previous, _ := ticketWatchdogFixture(t)
	account.Extra = map[string]any{openAICodexTicketExtraKey(previous.Model): previous}
	svc.invalidateOpenAICodexTicket(context.Background(), account, previous)
	require.Nil(t, svc.lookupOpenAICodexTicket(account, previous.Model))
	next := *previous
	next.CapturedAt = time.Now()
	require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, &next))
	require.Equal(t, next.CapturedAt, svc.lookupOpenAICodexTicket(account, next.Model).CapturedAt)
	require.False(t, svc.storeOpenAICodexTicket(context.Background(), account, previous))
}

func TestCodexTicketRevocationSnapshotClearsOtherInstanceMemory(t *testing.T) {
	svc, account, previous, _ := ticketWatchdogFixture(t)
	tombstone := *previous
	tombstone.Revoked = true
	account.Extra = map[string]any{openAICodexTicketExtraKey(previous.Model): &tombstone}
	require.Nil(t, svc.lookupOpenAICodexTicket(account, previous.Model))
	account.Extra[openAICodexTicketExtraKey(previous.Model)] = previous
	require.Nil(t, svc.lookupOpenAICodexTicket(account, previous.Model))
}

func TestCodexTicketRevocationOldTombstoneCannotLowerWatermark(t *testing.T) {
	svc, account, previous, _ := ticketWatchdogFixture(t)
	svc.invalidateOpenAICodexTicket(context.Background(), account, previous)
	older := *previous
	older.Revoked, older.CapturedAt = true, previous.CapturedAt.Add(-time.Minute)
	account.Extra = map[string]any{openAICodexTicketExtraKey(previous.Model): &older}
	require.Nil(t, svc.lookupOpenAICodexTicket(account, previous.Model))
	account.Extra[openAICodexTicketExtraKey(previous.Model)] = previous
	require.Nil(t, svc.lookupOpenAICodexTicket(account, previous.Model))
}

func TestCodexTicketWatchdogDoesNotAttributeUninjectedClientState(t *testing.T) {
	for _, kind := range []string{"ungated", "expired", "disabled"} {
		t.Run(kind, func(t *testing.T) {
			svc, account, ticket, req := ticketWatchdogFixture(t)
			req = req.WithContext(context.Background())
			svc.cfg.Gateway.OpenAICodexTicket.FailClosed = false
			switch kind {
			case "ungated":
				svc.cfg.Gateway.OpenAICodexTicket.Models = []string{"different-model"}
			case "expired":
				expired := *ticket
				expired.ExpiresAt = time.Now().Add(-time.Second)
				require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, &expired))
			case "disabled":
				svc.cfg.Gateway.OpenAICodexTicket.Enabled = false
			}
			require.NoError(t, svc.applyOpenAICodexTicketRequest(account, ticket.Model, req))
			require.Equal(t, ticket.State, req.Header.Get(openAICodexTurnStateHeader))
			require.Nil(t, req.Context().Value(openAICodexTicketReceiptKey{}))
		})
	}
}
