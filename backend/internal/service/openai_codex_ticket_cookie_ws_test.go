package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func codexCookieWSFixture(t *testing.T) (*OpenAIGatewayService, *Account, openAIWSAcquireRequest) {
	return codexCookieWSFixtureForPolicy(t, "cookie", true)
}

func codexCookieWSFixtureForPolicy(t *testing.T, mode string, failClosed bool) (*OpenAIGatewayService, *Account, openAIWSAcquireRequest) {
	t.Helper()
	s, account, request := codexCookieObservationFixture(t)
	ticket := *s.lookupOpenAICodexTicket(account, "gpt-6-astra")
	s.cfg.Gateway.OpenAICodexTicket.CredentialMode = mode
	s.cfg.Gateway.OpenAICodexTicket.FailClosed = failClosed
	ticket.CredentialMode = mode
	if mode == "cookie_state" {
		ticket.State, ticket.Length = fakeCodexTicketState(292), 292
	}
	ticket.Binding = s.codexTicketBindingForConfig(account, s.openAICodexTicketConfigForAccount(context.Background(), account))
	s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, ticket.Model), &ticket)
	receipt, err := s.applyOpenAICodexTicketSnapshot(context.Background(), account, ticket.Model, request.Header)
	require.NoError(t, err)
	require.NotNil(t, receipt)
	return s, account, openAIWSAcquireRequest{Account: account,
		WSURL: "wss://chatgpt.com/backend-api/codex/responses", Headers: request.Header.Clone(),
		CodexTicketReceipt: codexTicketWSReceiptFromSnapshot(receipt)}
}

func TestCodexTicketCookieWSPoolIsolatesCookieIdentityAndDialHeaders(t *testing.T) {
	s, account, req := codexCookieWSFixture(t)
	s.cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	s.cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	s.cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	pool := newOpenAIWSConnPool(s.cfg)
	t.Cleanup(pool.Close)
	dialer := &codexTicketPoolDialer{}
	pool.setClientDialerForTest(dialer)
	first, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	first.Release()
	next := req.CodexTicketReceipt.ticket
	next.Cookies = []*http.Cookie{{Name: "ticket_session", Value: "replacement-private", Path: "/backend-api", Secure: true, Expires: next.ExpiresAt}}
	req.CodexTicketReceipt = &openAICodexTicketWSReceipt{account: account, ticket: next, config: req.CodexTicketReceipt.config}
	req.Headers = req.Headers.Clone()
	req.Headers.Set("Cookie", "ticket_session=replacement-private")
	second, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.False(t, second.Reused(), "Cookie 改变必须使用新握手，不能依靠相同空 STATE 复用")
	require.NotEqual(t, first.ConnID(), second.ConnID())
	second.Release()
	req.HeadersFactory = func(_ context.Context, h http.Header) (http.Header, error) {
		h.Set("Cookie", "ticket_session=client-owned")
		return h, nil
	}
	pool.Close()
	pool = newOpenAIWSConnPool(s.cfg)
	t.Cleanup(pool.Close)
	pool.setClientDialerForTest(dialer)
	_, err = pool.Acquire(context.Background(), req)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	require.Len(t, dialer.headers, 2)
}

func TestCodexTicketCookieWSRefreshClearsExpiredManagedHeaders(t *testing.T) {
	s, account, req := codexCookieWSFixtureForPolicy(t, "cookie", false)
	expired := req.CodexTicketReceipt.ticket
	expired.ExpiresAt = time.Now().Add(-time.Second)
	s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, expired.Model), &expired)
	req.Headers.Add("Cookie", "client_cookie=preserve")
	require.NoError(t, s.refreshOpenAICodexTicketWSHeaders(context.Background(), account, expired.Model, &req))
	require.Nil(t, req.CodexTicketReceipt)
	require.Equal(t, "client_cookie=preserve", req.Headers.Get("Cookie"))
	require.Empty(t, req.Headers.Get(openAICodexTurnStateHeader))
}

func TestCodexTicketCookieWSFramesContinueExpiredOrRenewedSession(t *testing.T) {
	for _, renew := range []bool{false, true} {
		s, account, req := codexCookieWSFixtureForPolicy(t, "cookie", false)
		receipt := req.CodexTicketReceipt
		inner := &codexTicketWSFrames{frames: make(chan []byte, 1)}
		conn := s.observeOpenAICodexTicketWSFrames(context.Background(), inner, receipt)
		next := receipt.ticket
		if renew {
			next.CapturedAt = time.Now()
			next.Cookies = []*http.Cookie{{Name: "ticket_session", Value: "renewed-private", Path: "/backend-api", Secure: true, Expires: next.ExpiresAt}}
		} else {
			next.ExpiresAt = time.Now().Add(-time.Second)
		}
		s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, next.Model), &next)
		payload := []byte(`{"type":"response.create","model":"gpt-6-astra"}`)
		require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, payload))
		require.Equal(t, payload, inner.written[0])
		wrapped := conn.(*openAICodexTicketWSFrameConn)
		require.Same(t, receipt, wrapped.receipt, "现有连接不能伪装使用新 Cookie")
		require.True(t, wrapped.observationDisabled)
		require.Nil(t, wrapped.watchdog)
		require.False(t, inner.closed)
	}
}

func TestCodexTicketCookieWSFramesStillRejectAccountDisable(t *testing.T) {
	s, account, req := codexCookieWSFixture(t)
	latest := cloneOpenAICodexTicketAccount(account)
	latest.Status = "disabled"
	s.accountRepo = &codexTicketWSLatestRepo{latest: latest}
	inner := &codexTicketWSFrames{}
	conn := s.observeOpenAICodexTicketWSFrames(context.Background(), inner, req.CodexTicketReceipt)
	require.ErrorIs(t, conn.WriteFrame(context.Background(), coderws.MessageText,
		[]byte(`{"type":"response.create","model":"gpt-6-astra"}`)), ErrOpenAICodexTicketUnavailable)
	require.Empty(t, inner.written)
}

func TestCodexTicketCookieWSHandshakeIgnoresStateAndKeepsCookieDeletion(t *testing.T) {
	s, account, req := codexCookieWSFixture(t)
	writes := captureTicketInvalidations(s)
	receipt := req.CodexTicketReceipt
	receipt.observeHandshake(context.Background(), s,
		http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): {fakeCodexTicketState(312)}})
	require.Empty(t, writes)
	require.NotNil(t, s.lookupOpenAICodexTicket(account, receipt.ticket.Model))
	receipt.observeHandshake(context.Background(), s,
		http.Header{"Set-Cookie": {"ticket_session=; Path=/backend-api; Max-Age=-1; Secure"}})
	require.Empty(t, writes, "Cookie changes wait for scheduled business revalidation")
}
