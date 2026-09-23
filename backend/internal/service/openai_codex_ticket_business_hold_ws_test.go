package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketBusinessHoldRealWSPassthroughTurnLifetime(t *testing.T) {
	for _, messageType := range []coderws.MessageType{coderws.MessageText, coderws.MessageBinary} {
		t.Run(messageType.String(), func(t *testing.T) { checkCodexTicketBusinessWSTurnLifetime(t, messageType) })
	}
}

func checkCodexTicketBusinessWSTurnLifetime(t *testing.T, messageType coderws.MessageType) {
	t.Helper()
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 5
	cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 5
	cfg.Gateway.OpenAIFirstOutputTimeoutSeconds = 5
	cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true}
	account := ticketTestAccount(901)
	account.Status, account.Schedulable = StatusActive, true
	account.Concurrency = 1
	account.Extra = map[string]any{"openai_oauth_responses_websockets_v2_mode": OpenAIWSIngressModePassthrough}
	upstream := newStagedPassthroughConn()
	svc := newPassthroughLifecycleService(cfg, upstream)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server, serverErr := startPassthroughLifecycleServer(t, ctx, svc, account)
	defer server.Close()
	client := dialPassthroughLifecycleClient(t, server)
	defer func() { _ = client.CloseNow() }()
	select {
	case <-upstream.writes:
	case err := <-serverErr:
		t.Fatalf("websocket exited before first upstream write: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("websocket did not forward first turn")
	}
	require.Error(t, svc.checkCodexTicketBusinessIdle(ctx, account))
	upstream.frames <- stagedPassthroughFrame{messageType: messageType, payload: []byte(`{"type":"response.completed","response":{"id":"resp_first","model":"gpt-5.1","usage":{"input_tokens":1,"output_tokens":1}}}`)}
	_, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return svc.checkCodexTicketBusinessIdle(ctx, account) == nil }, time.Second, time.Millisecond)
	select {
	case err := <-serverErr:
		t.Fatalf("idle websocket closed unexpectedly: %v", err)
	default:
	}
	if messageType == coderws.MessageText {
		require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-5.1"}`)))
		requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
		require.Error(t, svc.checkCodexTicketBusinessIdle(ctx, account))
	}
	_ = client.CloseNow()
	cancel()
	select {
	case <-serverErr:
	case <-time.After(3 * time.Second):
		t.Fatal("websocket did not exit")
	}
	require.Eventually(t, func() bool { return svc.checkCodexTicketBusinessIdle(context.Background(), account) == nil }, time.Second, time.Millisecond)
}

type codexBusinessDrainConn struct {
	*openAIWSCancelSafeConn
	clientCtx context.Context
	draining  chan struct{}
	resume    chan struct{}
	once      sync.Once
}

func (c *codexBusinessDrainConn) ReadMessage(ctx context.Context) ([]byte, error) {
	if c.clientCtx.Err() != nil {
		c.once.Do(func() { close(c.draining) })
		select {
		case <-c.resume:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return c.openAIWSCancelSafeConn.ReadMessage(ctx)
}

func TestCodexTicketBusinessHoldHTTPWSDrainLifetime(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn := &codexBusinessDrainConn{
		openAIWSCancelSafeConn: &openAIWSCancelSafeConn{openAIWSCaptureConn: &openAIWSCaptureConn{events: [][]byte{
			[]byte(`{"type":"response.created","response":{"id":"drain","model":"gpt-5.1"}}`),
			[]byte(`{"type":"response.output_text.delta","delta":"partial"}`),
			[]byte(`{"type":"response.completed","response":{"id":"drain","model":"gpt-5.1","usage":{"input_tokens":3,"output_tokens":5}}}`),
		}}},
		clientCtx: ctx, draining: make(chan struct{}), resume: make(chan struct{}),
	}
	var resumeOnce sync.Once
	resume := func() { resumeOnce.Do(func() { close(conn.resume) }) }
	defer resume()
	cfg := passthroughLifecycleConfig()
	cfg.Gateway.OpenAIWS.OAuthEnabled = true
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
	cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(&openAIWSClientConnCancelDialer{conn: conn})
	defer pool.Close()
	svc := newPassthroughLifecycleService(cfg, nil)
	svc.openaiWSPool = pool
	account := ticketTestAccount(901)
	account.Status, account.Schedulable, account.Concurrency = StatusActive, true, 1
	writer := &cancelOnFirstWriteResponseWriter{cancel: cancel}
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := svc.Forward(ctx, c, account, []byte(`{"model":"gpt-5.1","stream":true,"input":[{"type":"input_text","text":"hello"}]}`))
		done <- err
	}()
	select {
	case <-conn.draining:
	case err := <-done:
		t.Fatalf("request exited before draining: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("request did not enter drain")
	}
	require.Never(t, func() bool { return svc.checkCodexTicketBusinessIdle(context.Background(), account) == nil }, 100*time.Millisecond, time.Millisecond)
	resume()
	require.NoError(t, <-done)
	require.NoError(t, svc.checkCodexTicketBusinessIdle(context.Background(), account))
}
