package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func codexCookiePendingEntryFixture(t *testing.T, mode string) (*OpenAIGatewayService, *Account, *http.Request) {
	t.Helper()
	s := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, CredentialMode: mode, CookieTTLSeconds: 60, FailClosed: true,
	}, nil)
	account := ticketTestAccount(41)
	now := time.Now()
	ticket := &openAICodexTicket{Model: "gpt-6-astra", CredentialMode: mode, Verified: true,
		CapturedAt: now, ExpiresAt: now.Add(time.Minute),
		Cookies: []*http.Cookie{{Name: "ticket_session", Value: "initial-private", Path: "/backend-api", Secure: true, Expires: now.Add(time.Minute)}}}
	if mode == config.CodexTicketCredentialCookieState {
		ticket.State, ticket.Length = fakeCodexTicketState(292), 292
	}
	ticket.Binding = s.codexTicketBindingForConfig(account, s.openAICodexTicketConfigForAccount(context.Background(), account))
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, ticket))
	req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	require.NoError(t, s.applyOpenAICodexTicketRequest(account, ticket.Model, req))
	require.Equal(t, "ticket_session=initial-private", req.Header.Get("Cookie"))
	return s, account, req
}

func TestCodexTicketPendingHTTPResponseCapturesCookieBeforeStatusAndStateChecks(t *testing.T) {
	for _, tc := range []struct {
		mode   string
		status int
	}{
		{config.CodexTicketCredentialCookie, http.StatusOK},
		{config.CodexTicketCredentialCookie, http.StatusTooManyRequests},
		{config.CodexTicketCredentialCookieState, http.StatusOK},
		{config.CodexTicketCredentialCookieState, http.StatusTooManyRequests},
		{config.CodexTicketCredentialCookieState, http.StatusBadGateway},
	} {
		t.Run(fmt.Sprintf("%s_%d", tc.mode, tc.status), func(t *testing.T) {
			s, account, req := codexCookiePendingEntryFixture(t, tc.mode)
			receipt := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
			before, headers := clonePendingCodexTicket(&receipt.ticket), req.Header.Clone()
			response := codexTicketCompletedResponse(receipt.ticket.Model, fakeCodexTicketState(312))
			response.StatusCode = tc.status
			response.Header.Add("Set-Cookie", "ticket_session=candidate-private; Path=/backend-api; Max-Age=120; Secure")
			s.observeOpenAICodexTicketResponse(req, response)
			_, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			pending := s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model)
			require.NotNil(t, pending)
			require.Equal(t, "ticket_session=candidate-private", codexTicketCookieHeaderValues(pending.Candidate.Jar.Cookies(req.URL)))
			require.Equal(t, before, pending.Ticket)
			require.Equal(t, before, &receipt.ticket)
			require.Equal(t, headers, req.Header)
			next, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
			require.NoError(t, err)
			require.NoError(t, s.applyOpenAICodexTicketRequest(account, receipt.ticket.Model, next))
			require.Equal(t, headers, next.Header, "未经业务复验的新 Cookie 不能进入后续业务请求")
		})
	}
}

func TestCodexTicketPendingWSHandshakeKeepsPublishedAndConnectionSnapshots(t *testing.T) {
	for _, mode := range []string{config.CodexTicketCredentialCookie, config.CodexTicketCredentialCookieState} {
		t.Run(mode, func(t *testing.T) {
			s, account, req := codexCookiePendingEntryFixture(t, mode)
			receipt := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
			ws := codexTicketWSReceiptFromSnapshot(receipt)
			before, identity := clonePendingCodexTicket(&ws.ticket), ws.identity()
			response := pendingCodexCookieResponse("ticket_session=ws-candidate; Path=/backend-api; Max-Age=120; Secure")
			ws.observeHandshake(context.Background(), s, response.Header)
			pending := s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model)
			require.NotNil(t, pending)
			require.Equal(t, "ticket_session=ws-candidate", codexTicketCookieHeaderValues(pending.Candidate.Jar.Cookies(req.URL)))
			require.Equal(t, before, pending.Ticket)
			require.Equal(t, before, &ws.ticket)
			require.Equal(t, identity, ws.identity())
			require.Equal(t, before, &receipt.ticket)
			headers := make(http.Header)
			next, err := s.applyOpenAICodexTicketSnapshot(context.Background(), account, receipt.ticket.Model, headers)
			require.NoError(t, err)
			require.Equal(t, identity, codexTicketWSReceiptFromSnapshot(next).identity())
			require.Equal(t, req.Header, headers)
		})
	}
}

func TestCodexTicketPendingResponseAfterSessionSoftExpiryStillCapturesCookies(t *testing.T) {
	for _, transport := range []string{"http", "websocket"} {
		t.Run(transport, func(t *testing.T) {
			s, account, req := codexCookiePendingEntryFixture(t, config.CodexTicketCredentialCookieState)
			receipt := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
			// 模拟请求已发送、响应到达前会话 Cookie 的配置期限已过，不等待真实时钟。
			now := time.Now()
			receipt.ticket.CapturedAt = now.Add(-2 * time.Minute)
			receipt.ticket.RevalidateAt = now.Add(-time.Minute)
			receipt.ticket.ExpiresAt = now.Add(-time.Minute)
			receipt.ticket.StateExpiresAt = now.Add(10 * time.Minute)
			receipt.ticket.Cookies[0].Expires = now.Add(-time.Minute)
			receipt.ticket.CookieSessionKeys = []string{codexTicketCookieKey(*receipt.ticket.Cookies[0])}
			before := clonePendingCodexTicket(&receipt.ticket)
			s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, receipt.ticket.Model), clonePendingCodexTicket(before))
			response := pendingCodexCookieResponse("ticket_session=late-candidate; Path=/backend-api; Secure")
			if transport == "http" {
				s.observeOpenAICodexTicketResponse(req, response)
			} else {
				ws := codexTicketWSReceiptFromSnapshot(receipt)
				ws.observeHandshake(context.Background(), s, response.Header)
				require.Equal(t, before, &ws.ticket)
			}
			pending := s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model)
			require.NotNil(t, pending, "在途请求响应的 Cookie 刷新不能因原会话软期限已过而丢失")
			require.Equal(t, "ticket_session=late-candidate", codexTicketCookieHeaderValues(pending.Candidate.Jar.Cookies(req.URL)))
			require.Equal(t, before, &receipt.ticket)
			require.Equal(t, "ticket_session=initial-private", req.Header.Get("Cookie"))
		})
	}
}

func TestCodexTicketPendingHTTPResponseIgnoresUnmanagedCookies(t *testing.T) {
	s, account, req := codexCookiePendingEntryFixture(t, config.CodexTicketCredentialCookieState)
	receipt := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	req.Header.Set("Cookie", "ticket_session=client-owned")
	s.observeOpenAICodexTicketResponse(req, pendingCodexCookieResponse("ticket_session=candidate; Path=/backend-api; Secure"))
	require.Nil(t, s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model))
	require.Equal(t, "client-owned", req.Cookies()[0].Value)
	require.Equal(t, "initial-private", s.lookupOpenAICodexTicket(account, receipt.ticket.Model).Cookies[0].Value)
}
