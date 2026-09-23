package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketWSPoolSeparatesSessionAndEgressIdentity(t *testing.T) {
	for _, changed := range []string{"session", "egress"} {
		t.Run(changed, func(t *testing.T) {
			account, cfg, ticket := codexSessionIdentityFixture(t, config.CodexTicketCredentialCookieState)
			svc := ticketTestService(t, cfg, nil)
			svc.cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
			svc.cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
			pool := newOpenAIWSConnPool(svc.cfg)
			t.Cleanup(pool.Close)
			dialer := &codexTicketPoolDialer{}
			pool.setClientDialerForTest(dialer)
			headers := http.Header{}
			ticket.applyHeaders(headers)
			firstReceipt := &openAICodexTicketWSReceipt{account: account, ticket: *ticket, config: cfg}
			req := openAIWSAcquireRequest{Account: account, WSURL: "wss://chatgpt.com/backend-api/codex/responses",
				Headers: headers, CodexTicketReceipt: firstReceipt}
			first, err := pool.Acquire(context.Background(), req)
			require.NoError(t, err)
			first.Release()
			next := codexTicketLeaf(ticket)
			if changed == "session" {
				next.SessionID = "session-two"
			} else {
				next.Egress = "egress-two"
			}
			req.Headers = headers.Clone()
			next.applyHeaders(req.Headers)
			req.CodexTicketReceipt = &openAICodexTicketWSReceipt{account: account, ticket: *next, config: cfg}
			require.NotEqual(t, firstReceipt.identity(), req.CodexTicketReceipt.identity())
			second, err := pool.Acquire(context.Background(), req)
			require.NoError(t, err)
			defer second.Release()
			require.False(t, second.Reused(), "STATE/Cookie 相同也不能复用不同 session 或出口的握手")
			require.NotEqual(t, first.ConnID(), second.ConnID())
			require.Equal(t, "session-one", firstReceipt.ticket.SessionID)
			require.Equal(t, "egress-one", firstReceipt.ticket.Egress)
			dialer.mu.Lock()
			defer dialer.mu.Unlock()
			require.Len(t, dialer.headers, 2)
			require.Equal(t, next.SessionID, dialer.headers[1].Get("session_id"))
		})
	}
}
