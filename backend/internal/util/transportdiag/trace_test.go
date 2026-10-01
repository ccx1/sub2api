package transportdiag

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type traceTestTLSConn struct{ net.Conn }

func (traceTestTLSConn) ConnectionState() tls.ConnectionState {
	return tls.ConnectionState{NegotiatedProtocol: "h2"}
}

func TestTraceComposesCallerTraceAndRedactsEvidence(t *testing.T) {
	var caller atomic.Int32
	ctx := httptrace.WithClientTrace(context.Background(), &httptrace.ClientTrace{
		GetConn: func(string) { caller.Add(1) },
		GotConn: func(httptrace.GotConnInfo) { caller.Add(1) },
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://secret-user:secret-password@private.example/?token=private-token", nil)
	diag := &Trace{}
	req = diag.Request(req)
	hooks := httptrace.ContextClientTrace(req.Context())
	hooks.GetConn("private.example:443")
	hooks.GotConn(httptrace.GotConnInfo{Conn: traceTestTLSConn{}, Reused: true, IdleTime: 250 * time.Millisecond})
	hooks.WroteHeaderField("Authorization", []string{"Bearer private-ticket"})
	hooks.TLSHandshakeDone(tls.ConnectionState{}, errors.New("tls: private-certificate"))
	if caller.Load() != 2 || FromContext(req.Context()) != diag {
		t.Fatal("caller trace or diagnostic context was lost")
	}
	snapshot := diag.Snapshot()
	if snapshot["protocol"] != "h2" || snapshot["connection_reused"] != true || snapshot["idle_ms"] != int64(250) {
		t.Fatalf("unexpected connection evidence: %v", snapshot)
	}
	raw, _ := json.Marshal(snapshot)
	for _, secret := range []string{"secret-user", "secret-password", "private.example", "private-token", "private-ticket", "private-certificate", "Authorization"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("snapshot contains secret %q", secret)
		}
	}
}

func TestTraceStagesPreservePositiveSendEvidence(t *testing.T) {
	diag := &Trace{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid", nil)
	req = diag.Request(req)
	hooks := httptrace.ContextClientTrace(req.Context())
	if diag.DefinitelyUnsent() || diag.MayHaveBeenSent() {
		t.Fatal("absence of trace is not proof of either state")
	}
	hooks.GetConn("ignored")
	if !diag.DefinitelyUnsent() {
		t.Fatal("dial without connection should prove unsent")
	}
	steps := []struct {
		phase string
		apply func()
	}{
		{"tls_handshake", hooks.TLSHandshakeStart},
		{"tls_complete", func() { hooks.TLSHandshakeDone(tls.ConnectionState{}, nil) }},
		{"connection_ready", func() { hooks.GotConn(httptrace.GotConnInfo{}) }},
		{"request_write", func() { hooks.WroteHeaderField("ignored", nil) }},
		{"awaiting_response_headers", func() { hooks.WroteRequest(httptrace.WroteRequestInfo{}) }},
		{"response_headers", hooks.GotFirstResponseByte},
		{"response_body", diag.MarkResponseBodyRead},
	}
	for _, step := range steps {
		step.apply()
		if phase := diag.Snapshot()["phase"]; phase != step.phase {
			t.Fatalf("phase = %v, want %s", phase, step.phase)
		}
	}
	hooks.GetConn("another-host")
	hooks.TLSHandshakeDone(tls.ConnectionState{}, errors.New("tls: failed reconnect"))
	if diag.DefinitelyUnsent() || !diag.MayHaveBeenSent() {
		t.Fatal("later reconnect erased prior send evidence")
	}
	if diag.Snapshot()["protocol"] != "unknown" {
		t.Fatal("missing ALPN must remain unknown")
	}
}

func TestTraceConcurrentCallbacksAndSnapshots(t *testing.T) {
	diag := &Trace{}
	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid", nil)
	hooks := httptrace.ContextClientTrace(diag.Request(req).Context())
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				hooks.GetConn("ignored")
				hooks.TLSHandshakeDone(tls.ConnectionState{}, errors.New("tls: ignored"))
				diag.MarkBodyRead()
				diag.MarkResponseBodyRead()
				_ = diag.Snapshot()
			}
		}()
	}
	wg.Wait()
	if diag.DefinitelyUnsent() || diag.Snapshot()["phase"] != "response_body" {
		t.Fatal("concurrent evidence did not survive")
	}
}
