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

func codexCookieObservationFixture(t *testing.T) (*OpenAIGatewayService, *Account, *http.Request) {
	t.Helper()
	s := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, CredentialMode: "cookie", CookieTTLSeconds: 60, FailClosed: true,
	}, nil)
	account := ticketTestAccount(41)
	expires := time.Now().Add(time.Minute)
	ticket := &openAICodexTicket{Model: "gpt-6-astra", CredentialMode: "cookie", Verified: true,
		CapturedAt: time.Now(), ExpiresAt: expires,
		Cookies: []*http.Cookie{{Name: "ticket_session", Value: "initial-private", Path: "/backend-api", Secure: true, Expires: expires}}}
	ticket.Binding = s.codexTicketBindingForConfig(account, s.openAICodexTicketConfigForAccount(context.Background(), account))
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, ticket))
	req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	require.NoError(t, s.applyOpenAICodexTicketRequest(account, ticket.Model, req))
	require.Equal(t, "ticket_session=initial-private", req.Header.Get("Cookie"))
	require.Empty(t, req.Header.Get(openAICodexTurnStateHeader))
	require.NotNil(t, req.Context().Value(openAICodexTicketReceiptKey{}))
	return s, account, req
}

func TestCodexTicketCookieWatchdogKeepsResponseAndRevokesMismatch(t *testing.T) {
	s, account, req := codexCookieObservationFixture(t)
	writes := captureTicketInvalidations(s)
	response := codexTicketCompletedResponse("gpt-5.6-luna", "")
	expected, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	response.Body = io.NopCloser(strings.NewReader(string(expected)))
	s.observeOpenAICodexTicketResponse(req, response)
	actual, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, expected, actual)
	require.Equal(t, "response_model_mismatch", awaitTicketInvalidation(t, writes).Invalidation.Reason)
	require.Nil(t, s.lookupOpenAICodexTicket(account, "gpt-6-astra"))
}

func TestCodexTicketCookieWatchdogIgnoresLegacyStateAndUnmanagedCookies(t *testing.T) {
	for _, changed := range []bool{false, true} {
		s, account, req := codexCookieObservationFixture(t)
		writes := captureTicketInvalidations(s)
		if changed {
			req.Header.Set("Cookie", "ticket_session=client-owned")
		}
		response := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312))
		if changed {
			response = codexTicketCompletedResponse("gpt-5.6-luna", "")
		}
		s.observeOpenAICodexTicketResponse(req, response)
		_, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		require.Empty(t, writes)
		require.NotNil(t, s.lookupOpenAICodexTicket(account, "gpt-6-astra"))
	}
}

func TestCodexTicketCookieWatchdogKeepsReceiptForResponseUpdatesAndDeletion(t *testing.T) {
	for _, value := range []string{
		"ticket_session=renewed-private; Path=/backend-api; Secure",
		"ticket_session=; Path=/backend-api; Max-Age=-1; Secure",
	} {
		s, _, req := codexCookieObservationFixture(t)
		writes := captureTicketInvalidations(s)
		response := &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Set-Cookie": {value}}}
		s.observeOpenAICodexTicketResponse(req, response)
		require.Empty(t, writes, "Cookie changes wait for scheduled business revalidation")
		require.Equal(t, http.StatusTooManyRequests, response.StatusCode)
	}
}

func TestCodexTicketCookieRequestDoesNotLeakToOtherTargets(t *testing.T) {
	s, account, _ := codexCookieObservationFixture(t)
	for _, target := range []string{
		"https://api.openai.com/v1/responses", "https://chatgpt.com/other",
		"http://chatgpt.com/backend-api/codex/responses", "https://chatgpt.com.evil.test/backend-api/codex/responses",
	} {
		req, err := http.NewRequest(http.MethodPost, target, nil)
		require.NoError(t, err)
		require.ErrorIs(t, s.applyOpenAICodexTicketRequest(account, "gpt-6-astra", req), ErrOpenAICodexTicketUnavailable)
		require.Empty(t, req.Header.Get("Cookie"), target)
		require.Nil(t, req.Context().Value(openAICodexTicketReceiptKey{}), target)
	}
}
