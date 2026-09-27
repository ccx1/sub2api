package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type bpsDiagnosticBody struct {
	readErr  error
	closeErr error
	closes   atomic.Int32
}

func (b *bpsDiagnosticBody) Read([]byte) (int, error) { return 0, b.readErr }
func (b *bpsDiagnosticBody) Close() error             { b.closes.Add(1); return b.closeErr }

func TestExcelBPSDiagnosticResponseReadAndClose(t *testing.T) {
	for _, tc := range []struct {
		name           string
		readErr        error
		cancelIncoming bool
	}{
		{name: "EOF", readErr: io.EOF},
		{name: "unexpected EOF", readErr: io.ErrUnexpectedEOF},
		{name: "child context canceled", readErr: context.Canceled},
		{name: "incoming context canceled", readErr: context.Canceled, cancelIncoming: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			incomingCtx, cancelIncoming := context.WithCancel(context.Background())
			defer cancelIncoming()
			requestCtx, cancelRequest := context.WithCancel(incomingCtx)
			defer cancelRequest()
			evidence := &excelBPSWriteEvidence{}
			req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, "https://example.invalid", nil)
			require.NoError(t, err)
			req = evidence.request(req)
			closeErr := errors.New("close failed")
			body := &bpsDiagnosticBody{readErr: tc.readErr, closeErr: closeErr}
			resp, err := evidence.observeResult(req, &http.Response{Body: body}, nil)
			require.NoError(t, err)
			if tc.cancelIncoming {
				cancelIncoming()
			} else if tc.readErr == context.Canceled {
				cancelRequest()
			}
			_, got := resp.Body.Read(make([]byte, 1))
			require.ErrorIs(t, got, tc.readErr)
			if tc.readErr == io.EOF {
				require.Equal(t, io.EOF, got, "normal EOF must retain its identity")
			} else if tc.readErr == context.Canceled {
				require.ErrorIs(t, requestCtx.Err(), context.Canceled)
			}
			require.ErrorIs(t, resp.Body.Close(), closeErr)
			require.Equal(t, int32(1), body.closes.Load())
			c := bpsTransportContext()
			c.Request = c.Request.WithContext(incomingCtx)
			recordExcelBPSTransportFailure(requestCtx, c, bpsTransportAccount(), "scope", "", got, "stream", 1, false, resp)
			if tc.cancelIncoming {
				require.ErrorIs(t, incomingCtx.Err(), context.Canceled)
				_, ok := c.Get(OpsUpstreamErrorsKey)
				require.False(t, ok)
				_, ok = c.Get(OpsUpstreamErrorMessageKey)
				require.False(t, ok)
				return
			}
			require.NoError(t, incomingCtx.Err(), "outbound cancellation must leave the incoming request active")
			value, ok := c.Get(OpsUpstreamErrorsKey)
			require.True(t, ok)
			events := value.([]*OpsUpstreamErrorEvent)
			require.Len(t, events, 1)
			var detail map[string]any
			require.NoError(t, json.Unmarshal([]byte(events[0].Detail), &detail))
			require.Equal(t, "response_body", detail["transport"].(map[string]any)["phase"])
		})
	}
}

func TestExcelBPSDiagnosticNeverReopensSentBody(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid", strings.NewReader("sensitive body"))
	evidence := &excelBPSWriteEvidence{}
	req = evidence.request(req)
	hooks := httptrace.ContextClientTrace(req.Context())
	hooks.GetConn("ignored")
	body, err := req.GetBody()
	require.NoError(t, err)
	_, err = body.Read(make([]byte, 1))
	require.NoError(t, err)
	require.NoError(t, body.Close())
	_, err = req.GetBody()
	require.ErrorContains(t, err, "may already have been sent")
	require.False(t, evidence.unsent())
}

func TestExcelBPSDiagnosticCancellationDoesNotRecordExitFailure(t *testing.T) {
	for _, cause := range []error{context.Canceled, io.ErrUnexpectedEOF} {
		t.Run(cause.Error(), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			c := bpsTransportContext()
			c.Request = c.Request.WithContext(ctx)
			recordExcelBPSTransportFailure(context.Background(), c, bpsTransportAccount(), "scope", "", cause, "stream", 1, false)
			_, ok := c.Get(OpsUpstreamErrorsKey)
			require.False(t, ok)
			_, ok = c.Get(OpsUpstreamErrorMessageKey)
			require.False(t, ok)
		})
	}
}

func TestExcelBPSDiagnosticEOFRecordsNativeAttempt(t *testing.T) {
	c, account := bpsTransportContext(), bpsTransportAccount()
	core, logs := observer.New(zap.WarnLevel)
	var existingTrace atomic.Int32
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
		WroteRequest: func(httptrace.WroteRequestInfo) { existingTrace.Add(1) },
	})
	ctx = logger.IntoContext(ctx, zap.New(core))
	calls := 0
	s := &OpenAIGatewayService{httpUpstream: &bpsTestUpstream{send: func(req *http.Request, _ string) (*http.Response, error) {
		calls++
		defer func() { _ = req.Body.Close() }()
		hooks := httptrace.ContextClientTrace(req.Context())
		hooks.GetConn("secret-proxy:443")
		hooks.GotConn(httptrace.GotConnInfo{})
		hooks.WroteHeaderField("Authorization", []string{"secret-ticket"})
		hooks.WroteHeaders()
		hooks.WroteRequest(httptrace.WroteRequestInfo{})
		return nil, fmt.Errorf("secret-error-payload: %w", io.EOF)
	}}}
	resp, _, err := s.doExcelBPSRequest(ctx, c, account, "secret-session", []byte("secret-payload"), "secret-ticket", "secret-account")
	require.Nil(t, resp)
	require.ErrorIs(t, err, io.EOF)
	require.Equal(t, 1, calls, "EOF after sending must never replay the POST")
	require.Equal(t, int32(1), existingTrace.Load())
	value, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	events := value.([]*OpsUpstreamErrorEvent)
	require.Len(t, events, 1)
	require.Equal(t, "unexpected_eof", events[0].Reason)
	require.Equal(t, account.ProxyID, events[0].ProxyID)
	var detail map[string]any
	require.NoError(t, json.Unmarshal([]byte(events[0].Detail), &detail))
	require.Equal(t, "awaiting_response_headers", detail["transport"].(map[string]any)["phase"])
	require.Equal(t, "awaiting_response_headers", logs.All()[0].ContextMap()["transport"].(map[string]any)["phase"])
	allDiagnostics := events[0].Detail + fmt.Sprint(logs.All()[0].ContextMap())
	for _, secret := range []string{"secret-proxy", "secret-ticket", "secret-error-payload", "secret-session", "secret-payload", "secret-account"} {
		require.NotContains(t, allDiagnostics, secret)
	}
}

func TestExcelBPSDiagnosticMissingTerminalRetainsBodyStage(t *testing.T) {
	evidence := &excelBPSWriteEvidence{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid", nil)
	req = evidence.request(req)
	resp, err := evidence.observeResult(req, &http.Response{Body: io.NopCloser(strings.NewReader(""))}, nil)
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	c := bpsTransportContext()
	recordExcelBPSTransportFailure(context.Background(), c, bpsTransportAccount(), "scope", "", nil, "stream", 1, false, resp)
	value, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	event := value.([]*OpsUpstreamErrorEvent)[0]
	require.Equal(t, "stream_incomplete", event.Reason)
	var detail map[string]any
	require.NoError(t, json.Unmarshal([]byte(event.Detail), &detail))
	require.Equal(t, "response_body", detail["transport"].(map[string]any)["phase"])
}
