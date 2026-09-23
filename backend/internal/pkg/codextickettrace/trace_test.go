package codextickettrace

import (
	"context"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecorderComposesTraceAndSanitizesOrigin(t *testing.T) {
	ctx, recorder := WithRecorder(context.Background())
	previousCalls := 0
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { previousCalls++ }})
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://user:password@example.com:443/path?token=secret", nil)
	require.NoError(t, err)
	traced, finish := Start(req)
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	httptrace.ContextClientTrace(traced.Context()).GotConn(httptrace.GotConnInfo{Conn: left})
	require.Equal(t, 1, previousCalls)
	before := recorder.Snapshot()
	require.NotNil(t, before)
	require.Nil(t, before.ResponseHeaderMS)
	finalURL, err := url.Parse("https://hidden:secret@target.example:8443/next?auth=private")
	require.NoError(t, err)
	finish(&http.Response{Proto: "HTTP/2.0", Request: &http.Request{URL: finalURL}})
	snapshot := recorder.Snapshot()
	require.NotNil(t, snapshot.ResponseHeaderMS)
	require.Equal(t, "https://target.example:8443", snapshot.FinalOrigin)
	require.Equal(t, "HTTP/2.0", snapshot.HTTPVersion)
	*snapshot.ResponseHeaderMS = -123
	require.GreaterOrEqual(t, *recorder.Snapshot().ResponseHeaderMS, int64(0))
}

func TestRecorderDoesNotInventFailedResponseMeasurements(t *testing.T) {
	ctx, recorder := WithRecorder(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)
	_, finish := Start(req)
	finish(nil)
	require.Nil(t, recorder.Snapshot())
	plain, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	require.NoError(t, err)
	unchanged, finish := Start(plain)
	require.Same(t, plain, unchanged)
	finish(&http.Response{})
	require.Nil(t, (*Recorder)(nil).Snapshot())
}
