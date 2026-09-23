package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codexTicketIngressDialer struct {
	mu      sync.Mutex
	headers []http.Header
	conns   []*openAIWSCaptureConn
}

func (d *codexTicketIngressDialer) Dial(_ context.Context, _ string, headers http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	conn := d.conns[len(d.headers)]
	d.headers = append(d.headers, headers.Clone())
	return conn, http.StatusSwitchingProtocols, nil, nil
}

func TestCodexTicketWSIngressRenewalBetweenTurns(t *testing.T) {
	for _, scenario := range []struct {
		name         string
		renew        bool
		revoke       bool
		continuation bool
	}{
		{"same ticket", false, false, false},
		{"standby preserves primary", true, false, false},
		{"continuation preserves primary", true, false, true},
		{"revoked primary switches to standby", true, true, false},
		{"continuation with revoked primary", true, true, true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, account, receipt := codexTicketWSFixture(t)
			account.Extra = map[string]any{"responses_websockets_v2_enabled": true, "openai_oauth_responses_websockets_v2_enabled": true}
			cfg := &s.cfg.Gateway.OpenAIWS
			cfg.Enabled, cfg.OAuthEnabled, cfg.ResponsesWebsocketsV2 = true, true, true
			cfg.MaxConnsPerAccount, cfg.MaxIdlePerAccount = 1, 1
			cfg.DialTimeoutSeconds, cfg.ReadTimeoutSeconds, cfg.WriteTimeoutSeconds = 3, 3, 3
			s.cache, s.toolCorrector = &stubGatewayCache{}, NewCodexToolCorrector()
			s.openaiWSResolver = NewOpenAIWSProtocolResolver(s.cfg)
			firstEvent := []byte(`{"type":"response.completed","response":{"id":"resp_first","status":"completed","model":"gpt-6-astra"}}`)
			secondEvent := []byte(`{"type":"response.completed","response":{"id":"resp_second","status":"completed","model":"gpt-6-astra"}}`)
			first := &openAIWSCaptureConn{events: [][]byte{firstEvent, secondEvent}}
			second := &openAIWSCaptureConn{events: [][]byte{secondEvent}}
			dialer := &codexTicketIngressDialer{conns: []*openAIWSCaptureConn{first, second}}
			s.openaiWSPool = newOpenAIWSConnPool(s.cfg)
			s.openaiWSPool.setClientDialerForTest(dialer)
			t.Cleanup(s.openaiWSPool.Close)
			client, done, turns := codexTicketIngressClient(t, s, account)
			write := func(body string) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(body)))
			}
			read := func(id string) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_, body, err := client.Read(ctx)
				require.NoError(t, err)
				require.Equal(t, id, gjson.GetBytes(body, "response.id").String())
				select {
				case <-turns:
				case <-ctx.Done():
					t.Fatal("turn completion callback timed out")
				}
			}
			write(`{"type":"response.create","model":"gpt-6-astra","store":false,"input":[{"type":"input_text","text":"hello"}]}`)
			read("resp_first")
			next := receipt.ticket
			if scenario.renew {
				next.State, next.CapturedAt = "gAAAAA"+strings.Repeat("N", 286), time.Now()
				require.True(t, s.storeOpenAICodexTicket(context.Background(), account, &next))
				require.Equal(t, receipt.ticket.State, s.lookupOpenAICodexTicket(account, next.Model).State)
			}
			if scenario.revoke {
				s.invalidateOpenAICodexTicket(context.Background(), account, &receipt.ticket)
				require.Equal(t, next.State, s.lookupOpenAICodexTicket(account, next.Model).State)
			}
			previous := ""
			if scenario.continuation {
				previous = `,"previous_response_id":"resp_first"`
			}
			write(`{"type":"response.create","model":"gpt-6-astra","store":false,"input":[{"type":"input_text","text":"world"}]` + previous + `}`)
			read("resp_second")
			require.NoError(t, client.Close(coderws.StatusNormalClosure, "done"))
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("ingress did not finish")
			}
			if !scenario.revoke {
				require.Len(t, dialer.headers, 1)
				require.Equal(t, receipt.ticket.State, dialer.headers[0].Get(openAICodexTurnStateHeader))
				require.Len(t, first.writes, 2)
				if scenario.continuation {
					require.Equal(t, "resp_first", first.writes[1]["previous_response_id"])
				}
				return
			}
			require.Len(t, dialer.headers, 2)
			require.Equal(t, next.State, dialer.headers[1].Get(openAICodexTurnStateHeader))
			require.Len(t, first.writes, 1, "换票后的请求不得写入原连接")
			require.Len(t, second.writes, 1)
			if scenario.continuation {
				require.NotContains(t, second.writes[0], "previous_response_id")
			}
		})
	}
}

func codexTicketIngressClient(t *testing.T, s *OpenAIGatewayService, account *Account) (*coderws.Conn, <-chan error, <-chan struct{}) {
	t.Helper()
	done, turns := make(chan error, 1), make(chan struct{}, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			done <- err
			return
		}
		defer conn.CloseNow()
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		_, first, err := conn.Read(ctx)
		if err != nil {
			done <- err
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r
		done <- s.ProxyResponsesWebSocketFromClient(ctx, c, conn, account, "tok", first,
			&OpenAIWSIngressHooks{AfterTurn: func(int, *OpenAIForwardResult, error) { turns <- struct{}{} }})
	}))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.CloseNow() })
	return client, done, turns
}
