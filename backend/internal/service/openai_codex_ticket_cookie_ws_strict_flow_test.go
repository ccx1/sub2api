package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketCookieWSStrictIngressRejectsExpiredBetweenTurns(t *testing.T) {
	for _, mode := range []string{"cookie", "cookie_state"} {
		t.Run(mode, func(t *testing.T) {
			s, account, dialer := codexCookieIngressFixtureForPolicy(t, mode, true)
			client, done, turns := codexTicketIngressClient(t, s, account)
			codexCookieIngressTurn(t, client, turns,
				`{"type":"response.create","model":"gpt-6-astra","store":false,"input":[{"type":"input_text","text":"hello"}]}`, "resp_first")
			expired := *s.lookupOpenAICodexTicket(account, "gpt-6-astra")
			expired.ExpiresAt = time.Now().Add(-time.Second)
			s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, expired.Model), &expired)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(
				`{"type":"response.create","model":"gpt-6-astra","store":false,"previous_response_id":"resp_first","input":[{"type":"input_text","text":"world"}]}`)))
			select {
			case err := <-done:
				require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
			case <-ctx.Done():
				t.Fatal("expired ticket did not stop ingress")
			}
			require.Len(t, dialer.headers, 1)
			require.Len(t, dialer.conns[0].writes, 1)
		})
	}
}

func TestCodexTicketCookieWSStrictIngressReconnectsWithValidReplacement(t *testing.T) {
	for _, mode := range []string{"cookie", "cookie_state"} {
		t.Run(mode, func(t *testing.T) {
			s, account, dialer := codexCookieIngressFixtureForPolicy(t, mode, true)
			client, done, turns := codexTicketIngressClient(t, s, account)
			codexCookieIngressTurn(t, client, turns,
				`{"type":"response.create","model":"gpt-6-astra","store":false,"input":[{"type":"input_text","text":"hello"}]}`, "resp_first")
			old := *s.lookupOpenAICodexTicket(account, "gpt-6-astra")
			s.invalidateOpenAICodexTicket(context.Background(), account, &old)
			next := old
			next.CapturedAt = time.Now()
			next.Cookies = []*http.Cookie{{Name: "ticket_session", Value: "replacement-private", Path: "/backend-api", Secure: true, Expires: next.ExpiresAt}}
			require.True(t, s.storeOpenAICodexTicket(context.Background(), account, &next))
			codexCookieIngressTurn(t, client, turns,
				`{"type":"response.create","model":"gpt-6-astra","store":false,"previous_response_id":"resp_first","input":[{"type":"input_text","text":"world"}]}`, "resp_second")
			require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("ingress did not finish")
			}
			require.Len(t, dialer.headers, 2)
			require.Equal(t, "ticket_session=replacement-private", dialer.headers[1].Get("Cookie"))
			require.Len(t, dialer.conns[0].writes, 1)
			require.Len(t, dialer.conns[1].writes, 1)
			require.NotContains(t, dialer.conns[1].writes[0], "previous_response_id")
		})
	}
}

func TestCodexTicketCookieWSStrictPrewarmMismatchBlocksBusiness(t *testing.T) {
	for _, mode := range []string{"cookie", "cookie_state"} {
		t.Run(mode, func(t *testing.T) {
			s, account, req := codexCookieWSFixtureForPolicy(t, mode, true)
			s.cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = true
			writes := captureTicketInvalidations(s)
			inner := &openAIWSCaptureConn{events: [][]byte{
				[]byte(`{"type":"response.completed","response":{"status":"completed","model":"gpt-5.6-luna"}}`),
			}}
			pool := newOpenAIWSConnPool(s.cfg)
			t.Cleanup(pool.Close)
			conn := newOpenAIWSConn("strict_cookie_prewarm", account.ID, inner, nil)
			conn.codexTicketReceipt = req.CodexTicketReceipt
			pool.getOrCreateAccountPool(account.ID).conns[conn.id] = conn
			lease := &openAIWSConnLease{conn: conn, pool: pool, accountID: account.ID}
			payload := map[string]any{"type": "response.create", "model": "gpt-6-astra"}
			err := s.performOpenAIWSGeneratePrewarm(context.Background(), lease,
				OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, payload, "", payload, account, nil, 0)
			require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
			require.Equal(t, "response_model_mismatch", awaitTicketInvalidation(t, writes).Invalidation.Reason)
			require.Error(t, lease.WriteJSONWithContextTimeout(context.Background(), payload, time.Second))
			require.Len(t, inner.writes, 1, "预热失败后不能发送真正业务")
			require.Equal(t, false, inner.writes[0]["generate"])
		})
	}
}

func TestCodexTicketCookieWSStrictLeaseSendRechecksRevocation(t *testing.T) {
	for _, mode := range []string{"cookie", "cookie_state"} {
		t.Run(mode, func(t *testing.T) {
			s, account, req := codexCookieWSFixtureForPolicy(t, mode, true)
			inner := &openAIWSCaptureConn{}
			conn := newOpenAIWSConn("strict_cookie_send", account.ID, inner, nil)
			t.Cleanup(conn.close)
			conn.codexTicketReceipt = req.CodexTicketReceipt
			lease := &openAIWSConnLease{conn: conn}
			s.invalidateOpenAICodexTicket(context.Background(), account, &req.CodexTicketReceipt.ticket)
			require.ErrorIs(t, lease.WriteJSONWithContextTimeout(context.Background(),
				map[string]any{"type": "response.create", "model": "gpt-6-astra"}, time.Second), ErrOpenAICodexTicketUnavailable)
			require.Empty(t, inner.writes)
		})
	}
}
