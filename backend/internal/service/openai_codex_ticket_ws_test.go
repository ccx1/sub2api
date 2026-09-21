package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func codexTicketWSFixture(t *testing.T) (*OpenAIGatewayService, *Account, *openAICodexTicketWSReceipt) {
	t.Helper()
	s := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	a := ticketTestAccount(41)
	ticket := &openAICodexTicket{Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292,
		CapturedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour)}
	require.True(t, s.storeOpenAICodexTicket(context.Background(), a, ticket))
	headers := http.Header{}
	snapshot, err := s.applyOpenAICodexTicketSnapshot(context.Background(), a, ticket.Model, headers)
	require.NoError(t, err)
	r := codexTicketWSReceiptFromSnapshot(snapshot)
	require.NotNil(t, r)
	return s, a, r
}

func TestCodexTicketWSReceiptUsesOnlyInjectionSnapshot(t *testing.T) {
	s, a, receipt := codexTicketWSFixture(t)
	require.Nil(t, codexTicketWSReceiptFromSnapshot(nil))

	// The receipt is a copy of the exact snapshot used to inject the header;
	// later policy/cache changes must not rebuild or erase it.
	state := receipt.ticket.State
	s.cfg.Gateway.OpenAICodexTicket.Enabled = false
	s.cfg.Gateway.OpenAICodexTicket.Models = []string{"other-model"}
	next := receipt.ticket
	next.State = fakeCodexTicketState(312)
	next.CapturedAt = time.Now()
	require.True(t, s.storeOpenAICodexTicket(context.Background(), a, &next))
	require.Equal(t, state, receipt.ticket.State)
	require.True(t, receipt.config.Enabled)
	require.Equal(t, receipt.ticket.Model, "gpt-6-astra")
	require.Nil(t, confirmedOpenAICodexTicketWSReceipt(receipt, nil))
	require.Nil(t, confirmedOpenAICodexTicketWSReceipt(receipt, &openAIWSConnLease{reused: true}))
	require.Nil(t, confirmedOpenAICodexTicketWSReceipt(receipt, &openAIWSConnLease{}))
	require.Same(t, receipt, confirmedOpenAICodexTicketWSReceipt(nil,
		&openAIWSConnLease{reused: true, conn: &openAIWSConn{codexTicketReceipt: receipt}}))
}

func TestCodexTicketWSObservationUsesImmutableReceipt(t *testing.T) {
	s, a, receipt := codexTicketWSFixture(t)
	newTicket := receipt.ticket
	newTicket.State = "gAAAAA" + strings.Repeat("C", 286)
	newTicket.CapturedAt = time.Now()
	require.True(t, s.storeOpenAICodexTicket(context.Background(), a, &newTicket))
	watch := receipt.watch(context.Background(), s, receipt.ticket.Model)
	// 同步执行撤票，确定迟到完成事件已经处理后再断言新票仍在。
	watch.invalidate = func() { s.invalidateOpenAICodexTicket(context.Background(), receipt.account, &receipt.ticket) }
	frame := []byte(`{"type":"response.completed","response":{"status":"completed","model":"gpt-other"}}`)
	original := append([]byte(nil), frame...)
	watch.observe(frame)
	require.Equal(t, original, frame)
	require.Equal(t, newTicket.State, s.lookupOpenAICodexTicket(a, newTicket.Model).State)
	require.NotEqual(t, newTicket.State, receipt.ticket.State)
}

type codexTicketWSRepo struct {
	AccountRepository
	updates chan struct{}
}

func (r *codexTicketWSRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	r.updates <- struct{}{}
	return nil
}

func TestCodexTicketWSHandshakeOnlyInvalidatesConfirmedReceipt(t *testing.T) {
	s, a, receipt := codexTicketWSFixture(t)
	result := http.Header{}
	result.Set(openAICodexTurnStateHeader, fakeCodexTicketState(312))
	var unknown *openAICodexTicketWSReceipt
	unknown.observeHandshake(context.Background(), s, result)
	receipt.observeHandshake(context.Background(), s, http.Header{})
	require.NotNil(t, s.lookupOpenAICodexTicket(a, receipt.ticket.Model))
	repo := &codexTicketWSRepo{updates: make(chan struct{}, 1)}
	s.accountRepo = repo
	receipt.observeHandshake(context.Background(), s, result)
	select {
	case <-repo.updates:
	case <-time.After(time.Second):
		t.Fatal("confirmed handshake rejection did not invalidate the sent ticket")
	}
	require.Nil(t, s.lookupOpenAICodexTicket(a, receipt.ticket.Model))
}

type codexTicketWSFrames struct {
	mu      sync.Mutex
	frames  chan []byte
	written [][]byte
	closed  bool
}

func (c *codexTicketWSFrames) ReadFrame(ctx context.Context) (coderws.MessageType, []byte, error) {
	select {
	case p, ok := <-c.frames:
		if !ok {
			return coderws.MessageText, nil, io.EOF
		}
		return coderws.MessageText, p, nil
	case <-ctx.Done():
		return coderws.MessageText, nil, ctx.Err()
	}
}

func (c *codexTicketWSFrames) WriteFrame(_ context.Context, _ coderws.MessageType, p []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.written = append(c.written, append([]byte(nil), p...))
	return nil
}

func (c *codexTicketWSFrames) Close() error { c.closed = true; return nil }

func TestCodexTicketWSFramesPreservePayloadAndInvalidateMismatch(t *testing.T) {
	s, a, receipt := codexTicketWSFixture(t)
	repo := &codexTicketWSRepo{updates: make(chan struct{}, 1)}
	s.accountRepo = repo
	inner := &codexTicketWSFrames{frames: make(chan []byte, 2)}
	conn := s.observeOpenAICodexTicketWSFrames(context.Background(), inner, receipt)
	request := []byte(`{"type":"response.create","model":"gpt-6-astra","input":[]}`)
	require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, request))
	frame := []byte(`{"type":"response.completed","response":{"status":"completed","model":"gpt-other"}}`)
	inner.frames <- frame
	kind, result, err := conn.ReadFrame(context.Background())
	require.NoError(t, err)
	require.Equal(t, coderws.MessageText, kind)
	require.Equal(t, frame, result)
	require.Equal(t, [][]byte{request}, inner.written)
	select {
	case <-repo.updates:
	case <-time.After(time.Second):
		t.Fatal("completed mismatch did not invalidate the sent ticket")
	}
	require.Nil(t, s.lookupOpenAICodexTicket(a, receipt.ticket.Model))
	require.NoError(t, conn.Close())
	require.True(t, inner.closed)
}

func TestCodexTicketWSFramesSkipUnknownModelsAndFailedResponses(t *testing.T) {
	for _, response := range []string{
		`{"type":"response.completed","response":{"status":"completed","model":"gpt-6-astra"}}`,
		`{"type":"response.failed","response":{"status":"failed","model":"gpt-other"}}`,
		`{"type":"response.completed","response":{"status":"completed"}}`,
		`{"type":"response.output_text.delta","delta":"text"}`,
	} {
		s, a, receipt := codexTicketWSFixture(t)
		watch := receipt.watch(context.Background(), s, receipt.ticket.Model)
		watch.invalidate = func() { t.Fatal("non-mismatch response invalidated ticket") }
		watch.observe([]byte(response))
		require.NotNil(t, s.lookupOpenAICodexTicket(a, receipt.ticket.Model))
		require.Nil(t, receipt.watch(context.Background(), s, "gpt-5.6-sol"))
	}
}

func TestCodexTicketWSLateFrameCannotRevokeConcurrentRenewal(t *testing.T) {
	s, a, receipt := codexTicketWSFixture(t)
	inner := &codexTicketWSFrames{frames: make(chan []byte)}
	conn := s.observeOpenAICodexTicketWSFrames(context.Background(), inner, receipt)
	require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText,
		[]byte(`{"type":"response.create","model":"gpt-6-astra"}`)))
	wrapped := conn.(*openAICodexTicketWSFrameConn)
	wrapped.watchdog.invalidate = func() { s.invalidateOpenAICodexTicket(context.Background(), receipt.account, &receipt.ticket) }
	done := make(chan error, 1)
	go func() { _, _, err := conn.ReadFrame(context.Background()); done <- err }()
	next := receipt.ticket
	next.State, next.CapturedAt = "gAAAAA"+strings.Repeat("C", 286), time.Now()
	require.True(t, s.storeOpenAICodexTicket(context.Background(), a, &next))
	inner.frames <- []byte(`{"type":"response.completed","response":{"model":"gpt-other","status":"completed"}}`)
	require.NoError(t, <-done)
	require.Equal(t, next.State, s.lookupOpenAICodexTicket(a, next.Model).State)
}

func TestCodexTicketWSConcurrentTurnsCannotReplaceReceipt(t *testing.T) {
	s, a, receipt := codexTicketWSFixture(t)
	inner := &codexTicketWSFrames{frames: make(chan []byte, 1)}
	conn := s.observeOpenAICodexTicketWSFrames(context.Background(), inner, receipt)
	request := []byte(`{"type":"response.create","model":"gpt-6-astra"}`)
	require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, request))
	require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, request))
	wrapped := conn.(*openAICodexTicketWSFrameConn)
	require.Nil(t, wrapped.watchdog)
	require.Same(t, receipt, wrapped.receipt)
	require.True(t, wrapped.observationDisabled)
	inner.frames <- []byte(`{"type":"response.completed","response":{"model":"gpt-other","status":"completed"}}`)
	_, _, err := conn.ReadFrame(context.Background())
	require.NoError(t, err)
	require.NotNil(t, s.lookupOpenAICodexTicket(a, receipt.ticket.Model))
}
