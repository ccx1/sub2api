package codextickettrace

import (
	"context"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync"
	"time"
)

type contextKey struct{}

// Snapshot 只记录连接事实；PeerAddr 是 socket 对端，不是代理公网出口。
type Snapshot struct {
	ResponseHeaderMS *int64 `json:"response_header_ms,omitempty"`
	PeerAddr         string `json:"peer_addr,omitempty"`
	HTTPVersion      string `json:"http_version,omitempty"`
	FinalOrigin      string `json:"final_origin,omitempty"`
}

type Recorder struct {
	mu    sync.Mutex
	value Snapshot
}

func WithRecorder(ctx context.Context) (context.Context, *Recorder) {
	r := &Recorder{}
	return context.WithValue(ctx, contextKey{}, r), r
}

func (r *Recorder) Snapshot() *Snapshot {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	v := r.value
	if v.ResponseHeaderMS != nil {
		ms := *v.ResponseHeaderMS
		v.ResponseHeaderMS = &ms
	}
	if v.ResponseHeaderMS == nil && v.PeerAddr == "" {
		return nil
	}
	return &v
}

// Start 在实际发送前调用；完成回调必须先于解压初始化和正文读取。
// WithClientTrace 会组合已有 trace，不能用新的上下文替换调用方打点。
func Start(req *http.Request) (*http.Request, func(*http.Response)) {
	if req == nil {
		return req, func(*http.Response) {}
	}
	r, _ := req.Context().Value(contextKey{}).(*Recorder)
	if r == nil {
		return req, func(*http.Response) {}
	}
	started := time.Now()
	trace := &httptrace.ClientTrace{GotConn: func(info httptrace.GotConnInfo) {
		if info.Conn == nil || info.Conn.RemoteAddr() == nil {
			return
		}
		addr := info.Conn.RemoteAddr().String()
		if len(addr) > 256 {
			return
		}
		r.mu.Lock()
		r.value.PeerAddr = addr
		r.mu.Unlock()
	}}
	traced := req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
	return traced, func(resp *http.Response) {
		if resp == nil {
			return
		}
		ms := max(0, time.Since(started).Milliseconds())
		target := req.URL
		if resp.Request != nil && resp.Request.URL != nil {
			target = resp.Request.URL
		}
		r.mu.Lock()
		r.value.ResponseHeaderMS = &ms
		r.value.HTTPVersion = resp.Proto
		r.value.FinalOrigin = origin(target)
		r.mu.Unlock()
	}
}

func origin(value *url.URL) string {
	if value == nil || (value.Scheme != "http" && value.Scheme != "https") || len(value.Host) > 256 {
		return ""
	}
	return value.Scheme + "://" + value.Host
}
