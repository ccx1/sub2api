package service

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"net/url"
	"strings"
	"syscall"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/transportdiag"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type bpsTestUpstream struct {
	httpUpstreamRecorder
	send func(*http.Request, string) (*http.Response, error)
}

func (u *bpsTestUpstream) Do(r *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	return u.send(r, proxy)
}

func bpsTransportContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	return c
}

func bpsTransportAccount() *Account {
	account := excelAccount()
	account.Proxy = &Proxy{ID: 91, Name: "primary", Protocol: "http", Host: "proxy.example", Port: 8080, FallbackMode: FallbackModeDirect}
	account.ProxyID = &account.Proxy.ID
	return account
}

func TestExcelBPSProxyFailoverOnlyBeforeRequestSent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		evidence func(*http.Request)
		retry    bool
	}{
		{"dial_failure", func(r *http.Request) { httptrace.ContextClientTrace(r.Context()).GetConn("bps.openai.com:443") }, true},
		{"missing_trace", func(*http.Request) {}, false},
		{"connection_obtained", func(r *http.Request) {
			tr := httptrace.ContextClientTrace(r.Context())
			tr.GetConn("bps.openai.com:443")
			tr.GotConn(httptrace.GotConnInfo{})
		}, false},
		{"header_started", func(r *http.Request) {
			tr := httptrace.ContextClientTrace(r.Context())
			tr.GetConn("bps.openai.com:443")
			tr.WroteHeaderField("Authorization", []string{"secret"})
		}, false},
		{"body_read_without_write_trace", func(r *http.Request) {
			httptrace.ContextClientTrace(r.Context()).GetConn("bps.openai.com:443")
			_, _ = r.Body.Read(make([]byte, 1))
		}, false},
		{"replay_body_read", func(r *http.Request) {
			httptrace.ContextClientTrace(r.Context()).GetConn("bps.openai.com:443")
			b, _ := r.GetBody()
			defer func() { _ = b.Close() }()
			_, _ = b.Read(make([]byte, 1))
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, account, calls := bpsTransportContext(), bpsTransportAccount(), 0
			upstream := &bpsTestUpstream{send: func(r *http.Request, proxy string) (*http.Response, error) {
				calls++
				defer func() { _ = r.Body.Close() }()
				if calls == 1 {
					tc.evidence(r)
					return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
				}
				require.Empty(t, proxy, "only explicitly configured direct fallback is allowed")
				b, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, "original body", string(b))
				require.Equal(t, "Bearer bearer-secret", r.Header.Get("Authorization"))
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("response"))}, nil
			}}
			s := &OpenAIGatewayService{httpUpstream: upstream}
			resp, _, err := s.doExcelBPSRequest(context.Background(), c, account, "private-session", []byte("original body"), "bearer-secret", "account-secret")
			if tc.retry {
				require.NoError(t, err)
				require.Equal(t, 2, calls)
				require.NoError(t, resp.Body.Close())
				_, set := c.Get(OpsUpstreamErrorMessageKey)
				require.False(t, set)
			} else {
				require.Error(t, err)
				require.Nil(t, resp)
				require.Equal(t, 1, calls)
			}
		})
	}
}

func TestExcelBPSNativeEgressNeverReplaysAcceptedOrPinnedRequests(t *testing.T) {
	for _, scenario := range []string{"cancelled", "deadline", "http_403", "response_and_error", "static_proxy", "pinned_attachment"} {
		t.Run(scenario, func(t *testing.T) {
			c, account, calls := bpsTransportContext(), bpsTransportAccount(), 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "static_proxy" {
				account.Proxy.FallbackMode = FallbackModeNone
			}
			if scenario == "pinned_attachment" {
				ctx = context.WithValue(ctx, excelBPSPinnedEgressKey{}, true)
			}
			s := &OpenAIGatewayService{httpUpstream: &bpsTestUpstream{send: func(r *http.Request, proxy string) (*http.Response, error) {
				calls++
				require.Equal(t, account.Proxy.URL(), proxy)
				defer func() { _ = r.Body.Close() }()
				httptrace.ContextClientTrace(r.Context()).GetConn("bps.openai.com:443")
				switch scenario {
				case "http_403":
					return &http.Response{StatusCode: 403, Body: io.NopCloser(strings.NewReader("denied"))}, nil
				case "response_and_error":
					return &http.Response{StatusCode: 502, Body: io.NopCloser(strings.NewReader("failed"))}, io.EOF
				case "cancelled":
					cancel()
					return nil, context.Canceled
				case "deadline":
					return nil, context.DeadlineExceeded
				default:
					return nil, syscall.ECONNREFUSED
				}
			}}}
			resp, _, err := s.doExcelBPSRequest(ctx, c, account, "scope", []byte("body"), "token", "account")
			if scenario == "http_403" {
				require.NoError(t, err)
				require.Equal(t, 403, resp.StatusCode)
				require.NoError(t, resp.Body.Close())
			} else {
				require.Error(t, err)
				require.Nil(t, resp)
			}
			require.Equal(t, 1, calls)
		})
	}
}

func TestExcelBPSTransportDiagnosticsAreCredentialFree(t *testing.T) {
	core, logs := observer.New(zap.WarnLevel)
	ctx := logger.IntoContext(context.Background(), zap.New(core).With(zap.String("request_id", "req-43885")))
	c := bpsTransportContext()
	account := bpsTransportAccount()
	err := &url.Error{Op: "Post", URL: "https://secret-user:proxy-password@bps.openai.com/?token=secret-token", Err: fmt.Errorf("credential=raw-secret: %w", syscall.ECONNRESET)}
	recordExcelBPSTransportFailure(ctx, c, account, "private-session-key", account.Proxy.URL(), err, "transport", 1, false)
	events, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	attempts, ok := events.([]*OpsUpstreamErrorEvent)
	require.True(t, ok)
	require.Len(t, attempts, 1)
	require.Equal(t, "connection_reset", attempts[0].Reason)
	require.NotNil(t, attempts[0].ProxyID)
	require.Equal(t, int64(91), *attempts[0].ProxyID)
	require.Equal(t, 0, attempts[0].UpstreamStatusCode)
	require.Equal(t, "req-43885", logs.All()[0].ContextMap()["request_id"])
	all := fmt.Sprint(attempts[0], logs.All()[0].ContextMap())
	for _, secret := range []string{"secret-user", "proxy-password", "secret-token", "raw-secret", "private-session-key", "proxy.example"} {
		require.NotContains(t, all, secret)
	}
}

func TestExcelBPSTransportErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{context.Canceled, "canceled"}, {context.DeadlineExceeded, "deadline_exceeded"},
		{&net.DNSError{Err: "private host"}, "dns_error"}, {syscall.ECONNREFUSED, "connection_refused"},
		{io.ErrUnexpectedEOF, "unexpected_eof"}, {errors.New("net/http: TLS handshake timeout"), "tls_handshake_timeout"},
		{errors.New("http2: connection lost"), "http2_error"}, {nil, "stream_incomplete"},
	} {
		require.Equal(t, tc.want, transportdiag.Classify(tc.err))
	}
}

// Exercise real net/http tracing through an in-memory peer, without external traffic.
func TestExcelBPSFailoverRealHTTPTransport(t *testing.T) {
	for _, sent := range []bool{false, true} {
		t.Run(fmt.Sprintf("sent_%t", sent), func(t *testing.T) {
			c, account, calls := bpsTransportContext(), bpsTransportAccount(), 0
			observed := make(chan string, 1)
			transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
				if !sent {
					return nil, &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}
				}
				local, remote := net.Pipe()
				go func() {
					defer func() { _ = remote.Close() }()
					req, err := http.ReadRequest(bufio.NewReader(remote))
					if err != nil {
						observed <- "read error"
						return
					}
					b, _ := io.ReadAll(req.Body)
					_ = req.Body.Close()
					observed <- string(b)
				}()
				return local, nil
			}}
			defer transport.CloseIdleConnections()
			client := &http.Client{Transport: transport}
			s := &OpenAIGatewayService{httpUpstream: &bpsTestUpstream{send: func(r *http.Request, _ string) (*http.Response, error) {
				calls++
				if calls == 2 {
					return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok"))}, nil
				}
				r.URL.Scheme = "http"
				return client.Do(r)
			}}}
			resp, _, err := s.doExcelBPSRequest(context.Background(), c, account, "session", []byte("must not duplicate"), "token", "account")
			if sent {
				require.Error(t, err)
				require.Equal(t, 1, calls)
				require.Equal(t, "must not duplicate", <-observed)
			} else {
				require.NoError(t, err)
				require.Equal(t, 2, calls)
				require.NoError(t, resp.Body.Close())
			}
		})
	}
}

func TestExcelBPSTransportAndStreamFailureAreServerErrors(t *testing.T) {
	for _, streamFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(streamFailure), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{err: io.EOF}
			if streamFailure {
				upstream.err = nil
				upstream.resp = &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_test\",\"status\":\"in_progress\"}}\n\n"))}
			}
			svc := openAIClientToolsTestService(upstream)
			body := []byte("{\"model\":\"gpt-6-astra\",\"stream\":true,\"input\":\"test\"}")
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
			_, err := svc.Forward(context.Background(), c, excelAccount(), body)
			require.Error(t, err)
			require.Contains(t, rec.Body.String(), "server_error")
			if streamFailure {
				require.Equal(t, 200, rec.Code)
				require.Contains(t, rec.Body.String(), "basispoints_stream_incomplete")
				_, marked := GetOpsStreamError(c)
				require.True(t, marked)
			} else {
				require.Equal(t, 502, rec.Code)
				require.Contains(t, rec.Body.String(), "basispoints_transport_error")
			}
		})
	}
}
