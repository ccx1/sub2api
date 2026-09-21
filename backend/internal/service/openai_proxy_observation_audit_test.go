package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type proxyObservationAuditWS struct{ observedProxyWSFrameStub }

func (*proxyObservationAuditWS) WriteFrame(context.Context, coderws.MessageType, []byte) error {
	return io.ErrUnexpectedEOF
}

func TestProxyObservationAuditWSTransportFailureCountsOnce(t *testing.T) {
	account := randomProxyAccount("")
	account.ID = 42
	proxyID := int64(7)
	account.ProxyID = &proxyID
	reporter := &randomProxyFailureStub{}
	inner := &proxyObservationAuditWS{observedProxyWSFrameStub{readErr: io.ErrUnexpectedEOF}}
	conn := &randomProxyObservedWSFrameConn{FrameConn: inner, account: account, source: reporter}
	require.Error(t, conn.WriteFrame(context.Background(), coderws.MessageText, []byte(`{"type":"ping"}`)))
	_, _, err := conn.ReadFrame(context.Background())
	require.Error(t, err)
	require.Len(t, reporter.proxies, 1, "同一坏连接的写错和读错只能累计一次")
}

func TestProxyObservationAuditLocalTLSFailureDoesNotPenalizePool(t *testing.T) {
	repo := &ticketResultProxyRepo{}
	repo.proxy = &Proxy{ID: 7, Status: StatusActive, Protocol: "http", Host: "pool.example", Port: 8080}
	account := ticketTestAccount(42)
	account.Extra = map[string]any{AntiDegradationExtraKey: true, "tls_fingerprint_builtin": "does-not-exist"}
	calls := 0
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, &codexTicketFuncUpstream{
		do: func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("must not send") },
	})
	svc.accountRepo = repo
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Zero(t, calls)
	require.Empty(t, repo.randomProxyFailureStub.proxies, "本地TLS配置错误没有出站，不能累计代理失败")
}

func TestProxyObservationAuditHTTPTransportAndFailoverCountOnce(t *testing.T) {
	repo := &ticketResultProxyRepo{}
	account := ticketTestAccount(42)
	account.Extra = map[string]any{ProxyModeExtraKey: ProxyModeRandom}
	proxyID := int64(7)
	account.ProxyID = &proxyID
	failure := errors.New("connection refused")
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, &codexTicketFuncUpstream{
		do: func(*http.Request) (*http.Response, error) { return nil, failure },
	})
	svc.accountRepo = repo
	req, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
	require.NoError(t, err)
	_, err = svc.doOpenAIUpstream(req, "", account)
	require.ErrorIs(t, err, failure)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	require.Error(t, svc.handleOpenAIUpstreamTransportError(req.Context(), c, account, err, false))
	require.Len(t, repo.randomProxyFailureStub.proxies, 1, "底层出站观察与上层failover处理只累计同一次故障")
}

func TestProxyObservationAuditHTTPStatusAndCompleteBody(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		sse     bool
		body    string
		failure int
		success int
	}{
		{"401", 401, false, `{}`, 0, 0},
		{"403", 403, false, `{}`, 0, 0},
		{"429", 429, false, `{}`, 0, 0},
		{"500", 500, false, `{}`, 1, 0},
		{"407", 407, false, `{}`, 1, 0},
		{"json_success", 200, false, `{"object":"response","status":"completed"}`, 0, 1},
		{"sse_success", 200, true, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"model\":\"gpt-6-astra\"}}\n\n", 0, 1},
		{"sse_without_ticket", 200, true, "data: {\"type\":\"response.created\"}\n\n", 0, 1},
		{"sse_auth", 200, true, "data: {\"type\":\"error\",\"error\":{\"code\":\"authentication_error\"}}\n\n", 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &ticketResultProxyRepo{}
			account := randomProxyAccount("")
			account.ID = 42
			proxyID := int64(7)
			account.ProxyID = &proxyID
			req, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
			require.NoError(t, err)
			resp := &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}
			if tc.sse {
				resp.Header.Set("Content-Type", "text/event-stream")
			}
			observeRandomProxyHTTPResult(req, account, repo, resp, nil)
			_, err = io.ReadAll(resp.Body)
			require.NoError(t, err)
			_, _ = resp.Body.Read(make([]byte, 1))
			require.NoError(t, resp.Body.Close())
			require.Len(t, repo.randomProxyFailureStub.proxies, tc.failure)
			require.Len(t, repo.randomProxySuccessStub.proxies, tc.success)
		})
	}
}

func TestProxyObservationAuditWSTerminalErrorCountsOncePerTurn(t *testing.T) {
	account := randomProxyAccount("")
	account.ID = 42
	proxyID := int64(7)
	account.ProxyID = &proxyID
	reporter := &randomProxyFailureStub{}
	inner := &observedProxyWSFrameStub{payload: []byte(`{"type":"error","error":{"status":500}}`)}
	conn := &randomProxyObservedWSFrameConn{FrameConn: inner, account: account, source: reporter}
	require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-astra"}`)))
	_, _, err := conn.ReadFrame(context.Background())
	require.NoError(t, err)
	inner.payload = []byte(`{"type":"response.failed","response":{"status":"failed","error":{"status":500}}}`)
	_, _, err = conn.ReadFrame(context.Background())
	require.NoError(t, err)
	inner.readErr = io.ErrUnexpectedEOF
	_, _, err = conn.ReadFrame(context.Background())
	require.Error(t, err)
	require.Len(t, reporter.proxies, 1, "同轮error、failed和关闭错误只能记一次失败")
}

func TestProxyObservationAuditTicketBodyChecksModelAndTruncation(t *testing.T) {
	for _, tc := range []struct{ name, body, state string }{
		{"truncated", "data: {\"type\":\"response.created\"}\n\n", ""},
		{"wrong_model", "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"model\":\"gpt-other\"}}\n\n", ""},
		{"rejected_state", "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"model\":\"gpt-6-astra\"}}\n\n", fakeCodexTicketState(312)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &ticketResultProxyRepo{}
			account := randomProxyAccount("")
			account.ID = 42
			proxyID := int64(7)
			account.ProxyID = &proxyID
			ctx := context.WithValue(context.Background(), openAICodexTicketReceiptKey{}, &openAICodexTicketReceipt{ticket: openAICodexTicket{Model: "gpt-6-astra"}})
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://example.test", nil)
			require.NoError(t, err)
			resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}
			resp.Header.Set("Content-Type", "text/event-stream")
			resp.Header.Set(openAICodexTurnStateHeader, tc.state)
			observeRandomProxyHTTPResult(req, account, repo, resp, nil)
			_, err = io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Len(t, repo.randomProxyFailureStub.proxies, 1)
			require.Empty(t, repo.randomProxySuccessStub.proxies)
		})
	}
}

func TestProxyObservationAuditSSETerminalReportsBeforeCloseWithoutEOF(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		fail, pass int
	}{
		{"completed", `{"type":"response.completed","response":{"status":"completed","model":"gpt-6-astra"}}`, 0, 1},
		{"server_error", `{"type":"response.failed","response":{"status":"failed","error":{"code":"server_error"}}}`, 1, 0},
		{"server_status", `{"type":"error","error":{"status":503}}`, 1, 0},
		{"401", `{"type":"error","error":{"status":401}}`, 0, 0},
		{"403", `{"type":"response.failed","response":{"status":"failed","error":{"status":403}}}`, 0, 0},
		{"429", `{"type":"error","error":{"status":429}}`, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &ticketResultProxyRepo{}
			account := randomProxyAccount("")
			account.ID = 42
			proxyID := int64(7)
			account.ProxyID = &proxyID
			req, err := http.NewRequest(http.MethodPost, "https://example.test", nil)
			require.NoError(t, err)
			body := "data: " + tc.body + "\n\n"
			resp := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
			observeRandomProxyHTTPResult(req, account, repo, resp, nil)
			n, err := resp.Body.Read(make([]byte, 4096))
			require.NoError(t, err, "测试尚未读取EOF")
			require.Equal(t, len(body), n)
			require.NoError(t, resp.Body.Close())
			require.Len(t, repo.randomProxyFailureStub.proxies, tc.fail)
			require.Len(t, repo.randomProxySuccessStub.proxies, tc.pass)
			_, _ = resp.Body.Read(make([]byte, 1))
			require.Len(t, repo.randomProxyFailureStub.proxies, tc.fail, "后续EOF不能重复上报")
			require.Len(t, repo.randomProxySuccessStub.proxies, tc.pass)
		})
	}
}
