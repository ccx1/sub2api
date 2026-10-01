package transportdiag

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
)

// Trace 仅保存白名单证据；跨内部重连累积的发送证据不能被后续失败抹掉。
type Trace struct {
	responseBodyRead                                               atomic.Bool
	started, connected, reused, tlsStarted, tlsCompleted           atomic.Bool
	headerStarted, wroteHeaders, wroteRequest, firstByte, bodyRead atomic.Bool
	handed                                                         atomic.Bool
	protocol                                                       atomic.Int32
	idleMillis                                                     atomic.Int64
	tlsError                                                       atomic.Value
}

type traceContextKey struct{}

func FromContext(ctx context.Context) *Trace {
	if ctx == nil {
		return nil
	}
	t, _ := ctx.Value(traceContextKey{}).(*Trace)
	return t
}

func (t *Trace) Context(ctx context.Context) context.Context {
	return context.WithValue(ctx, traceContextKey{}, t)
}

func (t *Trace) Request(req *http.Request) *http.Request {
	trace := &httptrace.ClientTrace{
		GetConn:           func(string) { t.started.Store(true) },
		TLSHandshakeStart: func() { t.tlsStarted.Store(true) },
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			if err == nil {
				t.tlsCompleted.Store(true)
			} else {
				t.tlsError.Store(Classify(err))
			}
		},
		GotConn: t.gotConn,
		WroteHeaderField: func(string, []string) {
			t.handed.Store(true)
			t.headerStarted.Store(true)
		},
		WroteHeaders: func() { t.handed.Store(true); t.wroteHeaders.Store(true) },
		WroteRequest: func(i httptrace.WroteRequestInfo) {
			t.handed.Store(true)
			if i.Err == nil {
				t.wroteRequest.Store(true)
			}
		},
		GotFirstResponseByte: func() { t.handed.Store(true); t.firstByte.Store(true) },
	}
	// WithClientTrace 合并调用者的 hooks，不替换原生重试/计时观察。
	return req.Clone(httptrace.WithClientTrace(t.Context(req.Context()), trace))
}

func (t *Trace) gotConn(info httptrace.GotConnInfo) {
	t.connected.Store(true)
	t.handed.Store(true)
	t.reused.Store(info.Reused)
	t.idleMillis.Store(info.IdleTime.Milliseconds())
	// 只读取实际交付连接的 ALPN；指纹连接不提供此接口时保留 unknown。
	protocol := int32(0)
	if conn, ok := info.Conn.(interface{ ConnectionState() tls.ConnectionState }); ok {
		switch conn.ConnectionState().NegotiatedProtocol {
		case "h2":
			protocol = 2
		case "http/1.1", "":
			protocol = 1
		}
	}
	t.protocol.Store(protocol)
}

func (t *Trace) MarkResponseBodyRead() { t.responseBodyRead.Store(true) }
func (t *Trace) MarkBodyRead()         { t.bodyRead.Store(true); t.handed.Store(true) }
func (t *Trace) DefinitelyUnsent() bool {
	return t.started.Load() && !t.handed.Load()
}
func (t *Trace) MayHaveBeenSent() bool { return t.handed.Load() }

func (t *Trace) phase() string {
	switch {
	case t.responseBodyRead.Load():
		return "response_body"
	case t.firstByte.Load():
		return "response_headers"
	case t.wroteRequest.Load():
		return "awaiting_response_headers"
	case t.headerStarted.Load() || t.wroteHeaders.Load() || t.bodyRead.Load():
		return "request_write"
	case t.connected.Load():
		return "connection_ready"
	case t.tlsCompleted.Load():
		return "tls_complete"
	case t.tlsStarted.Load():
		return "tls_handshake"
	case t.started.Load():
		return "connect"
	default:
		return "unobserved"
	}
}

func (t *Trace) Snapshot() map[string]any {
	protocol := "unknown"
	switch t.protocol.Load() {
	case 1:
		protocol = "http/1.1"
	case 2:
		protocol = "h2"
	}
	tlsError, _ := t.tlsError.Load().(string)
	return map[string]any{
		"phase": t.phase(), "protocol": protocol, "connection_obtained": t.connected.Load(),
		"connection_reused": t.reused.Load(), "idle_ms": t.idleMillis.Load(),
		"tls_started": t.tlsStarted.Load(), "tls_completed": t.tlsCompleted.Load(), "tls_error_kind": tlsError,
		"headers_started": t.headerStarted.Load(), "headers_written": t.wroteHeaders.Load(),
		"request_written": t.wroteRequest.Load(), "body_read": t.bodyRead.Load(),
		"first_response_byte": t.firstByte.Load(),
	}
}
