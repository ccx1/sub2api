package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketCookieWSStrictQueuedHandshakeExpiry(t *testing.T) {
	for _, mode := range []string{"cookie", "cookie_state"} {
		t.Run(mode, func(t *testing.T) {
			s, account, req := codexCookieWSFixtureForPolicy(t, mode, true)
			s.cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			s.cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			pool := newOpenAIWSConnPool(s.cfg)
			t.Cleanup(pool.Close)
			dialer := &codexTicketPoolDialer{}
			pool.setClientDialerForTest(dialer)
			ap := pool.getOrCreateAccountPool(account.ID)
			ap.creating = 1
			req.CodexTicketReceipt.ticket.ExpiresAt = time.Now().Add(50 * time.Millisecond)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				lease, err := pool.Acquire(ctx, req)
				if lease != nil {
					lease.Release()
				}
				done <- err
			}()
			<-time.After(time.Until(req.CodexTicketReceipt.ticket.ExpiresAt.Add(20 * time.Millisecond)))
			ap.mu.Lock()
			ap.creating = 0
			ap.signalChangedLocked()
			ap.mu.Unlock()
			require.ErrorIs(t, <-done, ErrOpenAICodexTicketUnavailable)
			require.Empty(t, dialer.headers, "排队耗尽票据有效期不能发起裸握手")
		})
	}
}

func TestCodexTicketCookieWSStrictHeadersFactoryInvalidation(t *testing.T) {
	for _, mode := range []string{"cookie", "cookie_state"} {
		for _, revoke := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/expired", true: "/revoked"}[revoke], func(t *testing.T) {
				s, account, req := codexCookieWSFixtureForPolicy(t, mode, true)
				pool := newOpenAIWSConnPool(s.cfg)
				t.Cleanup(pool.Close)
				dialer := &codexTicketPoolDialer{}
				pool.setClientDialerForTest(dialer)
				req.HeadersFactory = func(_ context.Context, headers http.Header) (http.Header, error) {
					if revoke {
						s.invalidateOpenAICodexTicket(context.Background(), account, &req.CodexTicketReceipt.ticket)
					} else {
						req.CodexTicketReceipt.ticket.Cookies[0].Expires = time.Now().Add(-time.Second)
					}
					return headers, nil
				}
				conn, err := pool.dialConn(context.Background(), req)
				if conn != nil {
					t.Cleanup(conn.close)
				}
				require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
				require.Nil(t, conn)
				require.Empty(t, dialer.headers)
			})
		}
	}
}

func TestCodexTicketCookieWSStrictQueuedLeaseRevocation(t *testing.T) {
	for _, mode := range []string{"cookie", "cookie_state"} {
		t.Run(mode, func(t *testing.T) {
			s, account, req := codexCookieWSFixtureForPolicy(t, mode, true)
			s.cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			s.cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
			pool := newOpenAIWSConnPool(s.cfg)
			t.Cleanup(pool.Close)
			dialer := &codexTicketPoolDialer{}
			pool.setClientDialerForTest(dialer)
			first, err := pool.Acquire(context.Background(), req)
			require.NoError(t, err)
			defer first.Release()
			req.PreferredConnID, req.ForcePreferredConn = first.ConnID(), true
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				lease, err := pool.Acquire(ctx, req)
				if lease != nil {
					lease.Release()
				}
				done <- err
			}()
			require.Eventually(t, func() bool { return first.conn.waiters.Load() > 0 }, time.Second, time.Millisecond)
			s.invalidateOpenAICodexTicket(context.Background(), account, &req.CodexTicketReceipt.ticket)
			first.Release()
			require.ErrorIs(t, <-done, ErrOpenAICodexTicketUnavailable)
			require.False(t, first.conn.isLeased(), "拒绝后必须归还连接租约")
			require.Len(t, dialer.headers, 1)
		})
	}
}

func TestCodexTicketCookieWSStrictFramesStopNewSendAndDrainExisting(t *testing.T) {
	for _, mode := range []string{"cookie", "cookie_state"} {
		t.Run(mode, func(t *testing.T) {
			s, account, req := codexCookieWSFixtureForPolicy(t, mode, true)
			inner := &codexTicketWSFrames{frames: make(chan []byte, 1)}
			conn := s.observeOpenAICodexTicketWSFrames(context.Background(), inner, req.CodexTicketReceipt)
			payload := []byte(`{"type":"response.create","model":"gpt-6-astra"}`)
			require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, payload))
			s.invalidateOpenAICodexTicket(context.Background(), account, &req.CodexTicketReceipt.ticket)
			completed := []byte(`{"type":"response.completed","response":{"status":"completed","model":"gpt-6-astra"}}`)
			inner.frames <- completed
			_, body, err := conn.ReadFrame(context.Background())
			require.NoError(t, err)
			require.Equal(t, completed, body)
			require.ErrorIs(t, conn.WriteFrame(context.Background(), coderws.MessageText, payload), ErrOpenAICodexTicketUnavailable)
			require.Len(t, inner.written, 1, "已发请求可收尾，下一次业务不能发送")
		})
	}
}
