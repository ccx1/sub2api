package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func codexCookieIngressFixture(t *testing.T) (*OpenAIGatewayService, *Account, *codexTicketIngressDialer) {
	return codexCookieIngressFixtureForPolicy(t, "cookie", true)
}

func codexCookieIngressFixtureForPolicy(t *testing.T, mode string, failClosed bool) (*OpenAIGatewayService, *Account, *codexTicketIngressDialer) {
	t.Helper()
	s, account, _ := codexCookieWSFixtureForPolicy(t, mode, failClosed)
	account.Extra = map[string]any{"responses_websockets_v2_enabled": true, "openai_oauth_responses_websockets_v2_enabled": true}
	cfg := &s.cfg.Gateway.OpenAIWS
	cfg.Enabled, cfg.OAuthEnabled, cfg.ResponsesWebsocketsV2 = true, true, true
	cfg.MaxConnsPerAccount, cfg.MaxIdlePerAccount = 1, 1
	cfg.DialTimeoutSeconds, cfg.ReadTimeoutSeconds, cfg.WriteTimeoutSeconds = 3, 3, 3
	s.cache, s.toolCorrector = &stubGatewayCache{}, NewCodexToolCorrector()
	s.openaiWSResolver = NewOpenAIWSProtocolResolver(s.cfg)
	first := []byte(`{"type":"response.completed","response":{"id":"resp_first","status":"completed","model":"gpt-6-astra"}}`)
	second := []byte(`{"type":"response.completed","response":{"id":"resp_second","status":"completed","model":"gpt-6-astra"}}`)
	dialer := &codexTicketIngressDialer{conns: []*openAIWSCaptureConn{
		{events: [][]byte{first, second}}, {events: [][]byte{second}},
	}}
	s.openaiWSPool = newOpenAIWSConnPool(s.cfg)
	s.openaiWSPool.setClientDialerForTest(dialer)
	t.Cleanup(s.openaiWSPool.Close)
	return s, account, dialer
}

func codexCookieIngressTurn(t *testing.T, client *coderws.Conn, turns <-chan struct{}, payload, responseID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(payload)))
	_, body, err := client.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, responseID, gjson.GetBytes(body, "response.id").String())
	select {
	case <-turns:
	case <-ctx.Done():
		t.Fatal("turn completion callback timed out")
	}
}

func TestCodexTicketCookieWSIngressKeepsContinuationAcrossCredentialChange(t *testing.T) {
	for _, renew := range []bool{false, true} {
		s, account, dialer := codexCookieIngressFixtureForPolicy(t, "cookie", false)
		client, done, turns := codexTicketIngressClient(t, s, account)
		codexCookieIngressTurn(t, client, turns,
			`{"type":"response.create","model":"gpt-6-astra","store":false,"input":[{"type":"input_text","text":"hello"}]}`, "resp_first")
		next := *s.lookupOpenAICodexTicket(account, "gpt-6-astra")
		if renew {
			next.CapturedAt = time.Now()
			next.Cookies = []*http.Cookie{{Name: "ticket_session", Value: "renewed-private", Path: "/backend-api", Secure: true, Expires: next.ExpiresAt}}
		} else {
			next.ExpiresAt = time.Now().Add(-time.Second)
		}
		s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, next.Model), &next)
		codexCookieIngressTurn(t, client, turns,
			`{"type":"response.create","model":"gpt-6-astra","store":false,"previous_response_id":"resp_first","input":[{"type":"input_text","text":"world"}]}`, "resp_second")
		require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("ingress did not finish")
		}
		require.Len(t, dialer.headers, 1, "Cookie 恢复变化不能拆断当前续会话")
		require.Equal(t, "ticket_session=initial-private", dialer.headers[0].Get("Cookie"))
		require.Len(t, dialer.conns[0].writes, 2)
		require.Equal(t, "resp_first", dialer.conns[0].writes[1]["previous_response_id"])
	}
}

func TestCodexTicketCookieWSPrewarmMismatchStillAllowsBusiness(t *testing.T) {
	s, account, req := codexCookieWSFixtureForPolicy(t, "cookie", false)
	s.cfg.Gateway.OpenAIWS.PrewarmGenerateEnabled = true
	writes := captureTicketInvalidations(s)
	inner := &openAIWSCaptureConn{events: [][]byte{
		[]byte(`{"type":"response.completed","response":{"status":"completed","model":"gpt-5.6-luna"}}`),
	}}
	pool := newOpenAIWSConnPool(s.cfg)
	t.Cleanup(pool.Close)
	conn := newOpenAIWSConn("cookie_prewarm", account.ID, inner, nil)
	conn.codexTicketReceipt = req.CodexTicketReceipt
	pool.getOrCreateAccountPool(account.ID).conns[conn.id] = conn
	lease := &openAIWSConnLease{conn: conn, pool: pool, accountID: account.ID}
	payload := map[string]any{"type": "response.create", "model": "gpt-6-astra"}
	require.NoError(t, s.performOpenAIWSGeneratePrewarm(context.Background(), lease,
		OpenAIWSProtocolDecision{Transport: OpenAIUpstreamTransportResponsesWebsocketV2}, payload, "", payload, account, nil, 0))
	require.Equal(t, "response_model_mismatch", awaitTicketInvalidation(t, writes).Invalidation.Reason)
	require.True(t, lease.IsPrewarmed())
	require.NoError(t, lease.WriteJSONWithContextTimeout(context.Background(), payload, time.Second))
	require.Len(t, inner.writes, 2)
}
