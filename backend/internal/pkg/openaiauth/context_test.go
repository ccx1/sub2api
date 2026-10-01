package openaiauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type contextRoundTripper func(*http.Request) (*http.Response, error)

type finalReadErrorBody struct {
	data string
	err  error
}

func (b *finalReadErrorBody) Read(p []byte) (int, error) {
	n := copy(p, b.data)
	b.data = b.data[n:]
	return n, b.err
}
func (*finalReadErrorBody) Close() error { return nil }

func TestResponseDataRejectsCompleteJSONWithReadError(t *testing.T) {
	for _, readErr := range []error{context.Canceled, context.DeadlineExceeded, io.ErrUnexpectedEOF} {
		t.Run(readErr.Error(), func(t *testing.T) {
			resp := &http.Response{StatusCode: http.StatusOK, Body: &finalReadErrorBody{
				data: `{"access_token":"test-token","refresh_token":"test-refresh"}`, err: readErr,
			}}
			value, err := responseData(resp)
			wantErr := readErr
			if errors.Is(readErr, io.ErrUnexpectedEOF) {
				wantErr = errNetwork
			}
			if value != nil || !errors.Is(err, wantErr) {
				t.Fatalf("incomplete read returned value=%v, err=%v; want %v", value, err, wantErr)
			}
		})
	}
}

func TestResponseDataRejectsCanceledRequestWithCompleteBody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, authOrigin+"/oauth/token", nil)
	resp := &http.Response{StatusCode: http.StatusOK, Request: req,
		Body: io.NopCloser(strings.NewReader(`{"access_token":"test-token","refresh_token":"test-refresh"}`))}
	value, err := responseData(resp)
	if value != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled response returned value=%v, err=%v", value, err)
	}
}

func TestLoginPreservesSentinelCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tr := &transport{ctx: ctx, deadline: time.Now().Add(time.Minute), client: &http.Client{
		Transport: contextRoundTripper(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host == "sentinel.openai.com" {
				cancel()
				return nil, ctx.Err()
			}
			finalRequest := req.Clone(req.Context())
			finalRequest.URL.Path = "/log-in"
			finalRequest.URL.RawQuery = ""
			return &http.Response{StatusCode: http.StatusOK, Request: finalRequest,
				Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
		}),
	}}
	result, err := login(tr, "test@example.com", "test-password", "JBSWY3DPEHPK3PXP", "test-workspace", "state", "verifier")
	if result != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("sentinel cancellation returned result=%v, err=%v", result, err)
	}
}

func TestAuthContextDeadlineRemainsTransient(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	creds, err := LoginContext(ctx, LoginInput{WorkspaceID: "test-workspace"})
	if creds != nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired login returned creds=%v, err=%v", creds, err)
	}
	tr := &transport{client: &http.Client{Transport: contextRoundTripper(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusUnauthorized, Header: make(http.Header),
			Body: canceledProbeBody{req.Context()}}, nil
	})}}
	result := tr.probeUsage(ctx, "test-token", "test-workspace")
	if result.Active || result.AuthFail {
		t.Fatalf("expired probe must remain transient: %+v", result)
	}
}

func (f contextRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLoginContextRejectsCancellationBeforeNetwork(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := LoginContext(ctx, LoginInput{WorkspaceID: "test-workspace", ProxyURL: ":invalid"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("login error = %v, want context cancellation", err)
	}
	result := ProbeContext(ctx, map[string]string{"access_token": "test-token", "workspace_id": "test-workspace"}, ":invalid")
	if result.Active || result.AuthFail || !strings.Contains(result.Detail, "canceled") {
		t.Fatalf("canceled probe must be transient: %+v", result)
	}
}

func TestTransportContextCancelsInFlightLoginRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	tr := &transport{ctx: ctx, deadline: time.Now().Add(time.Minute), client: &http.Client{
		Transport: contextRoundTripper(func(req *http.Request) (*http.Response, error) {
			close(started)
			<-req.Context().Done()
			return nil, req.Context().Err()
		}),
	}}
	req, err := tr.newRequest(http.MethodPost, authOrigin+"/oauth/token", nil)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := tr.do(req); done <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("login request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("login request error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("login request ignored cancellation")
	}
}

type canceledProbeBody struct{ ctx context.Context }

func (b canceledProbeBody) Read([]byte) (int, error) { <-b.ctx.Done(); return 0, b.ctx.Err() }
func (b canceledProbeBody) Close() error             { return nil }

func TestProbeContextCancelsResponseBody(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	tr := &transport{client: &http.Client{Transport: contextRoundTripper(func(req *http.Request) (*http.Response, error) {
		close(started)
		return &http.Response{StatusCode: http.StatusOK, Body: canceledProbeBody{req.Context()}, Header: make(http.Header)}, nil
	})}}
	done := make(chan ProbeResult, 1)
	go func() { done <- tr.probeUsage(ctx, "test-token", "test-workspace") }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not start")
	}
	cancel()
	select {
	case result := <-done:
		if result.Active || result.AuthFail {
			t.Fatalf("canceled response body must be transient: %+v", result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("probe body ignored cancellation")
	}
}

func TestProbeUsageKeepsSuccessfulAndUnauthorizedResults(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusUnauthorized} {
		tr := &transport{client: &http.Client{Transport: contextRoundTripper(func(req *http.Request) (*http.Response, error) {
			if req.Header.Get("Authorization") != "Bearer test-token" || req.Header.Get("ChatGPT-Account-Id") != "test-workspace" {
				t.Error("probe lost account credentials")
			}
			return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(`{"account_id":"test-workspace"}`)), Header: make(http.Header)}, nil
		})}}
		result := tr.probeUsage(context.Background(), "test-token", "test-workspace")
		if result.Active != (status == http.StatusOK) || result.AuthFail != (status == http.StatusUnauthorized) {
			t.Fatalf("unexpected status %d result: %+v", status, result)
		}
	}
}
