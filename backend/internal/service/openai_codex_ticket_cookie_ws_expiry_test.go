package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketCookieWSQueuedNewHandshakeDropsExpiredCredentials(t *testing.T) {
	s, account, req := codexCookieWSFixtureForPolicy(t, "cookie", false)
	s.cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	s.cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	s.cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	pool := newOpenAIWSConnPool(s.cfg)
	t.Cleanup(pool.Close)
	dialer := &codexTicketPoolDialer{}
	pool.setClientDialerForTest(dialer)
	ap := pool.getOrCreateAccountPool(account.ID)
	ap.creating = 1
	req.CodexTicketReceipt.ticket.ExpiresAt = time.Now().Add(50 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	type result struct {
		lease *openAIWSConnLease
		err   error
	}
	done := make(chan result, 1)
	go func() {
		lease, err := pool.Acquire(ctx, req)
		done <- result{lease, err}
	}()
	<-time.After(time.Until(req.CodexTicketReceipt.ticket.ExpiresAt.Add(20 * time.Millisecond)))
	ap.mu.Lock()
	ap.creating = 0
	ap.signalChangedLocked()
	ap.mu.Unlock()
	got := <-done
	require.NoError(t, got.err)
	require.NotNil(t, got.lease)
	defer got.lease.Release()
	require.Nil(t, got.lease.conn.codexTicketReceipt)
	require.Len(t, dialer.headers, 1)
	require.Empty(t, dialer.headers[0].Get("Cookie"))
	require.Empty(t, dialer.headers[0].Get(openAICodexTurnStateHeader))
	unmanaged := req
	unmanaged.Headers, unmanaged.CodexTicketReceipt = dialer.headers[0], nil
	require.Equal(t, normalizeOpenAIWSTicketCompatibility(unmanaged), got.lease.conn.handshakeCompatibility)
	require.Equal(t, "ticket_session=initial-private", req.Headers.Get("Cookie"), "清理不能改写调用方的旧握手快照")
}

func TestCodexTicketCookieWSExpiryAfterHeadersFactoryFailsOpen(t *testing.T) {
	s, _, req := codexCookieWSFixtureForPolicy(t, "cookie", false)
	pool := newOpenAIWSConnPool(s.cfg)
	t.Cleanup(pool.Close)
	dialer := &codexTicketPoolDialer{}
	pool.setClientDialerForTest(dialer)
	req.HeadersFactory = func(_ context.Context, headers http.Header) (http.Header, error) {
		req.CodexTicketReceipt.ticket.Cookies[0].Expires = time.Now().Add(-time.Second)
		return headers, nil
	}
	conn, err := pool.dialConn(context.Background(), req)
	require.NoError(t, err)
	t.Cleanup(conn.close)
	require.Nil(t, conn.codexTicketReceipt)
	require.Empty(t, dialer.headers[0].Get("Cookie"))
}

func TestCodexTicketCookieWSExpiredReceiptCannotCrossAccounts(t *testing.T) {
	s, _, req := codexCookieWSFixture(t)
	req.CodexTicketReceipt.ticket.ExpiresAt = time.Now().Add(-time.Second)
	req.Account = ticketTestAccount(42)
	pool := newOpenAIWSConnPool(s.cfg)
	t.Cleanup(pool.Close)
	dialer := &codexTicketPoolDialer{}
	pool.setClientDialerForTest(dialer)
	_, err := pool.Acquire(context.Background(), req)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	_, err = pool.dialConn(context.Background(), req)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	require.Empty(t, dialer.headers)
}
