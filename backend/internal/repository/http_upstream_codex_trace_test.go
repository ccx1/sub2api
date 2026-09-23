package repository

import (
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/codextickettrace"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketTraceMeasuredBeforeGzipBodyArrives(t *testing.T) {
	for _, tlsEntry := range []bool{false, true} {
		t.Run(map[bool]string{false: "Do", true: "DoWithTLSFallback"}[tlsEntry], func(t *testing.T) {
			release := make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Encoding", "gzip")
				w.WriteHeader(200)
				w.(http.Flusher).Flush()
				select {
				case <-release:
				case <-r.Context().Done():
					return
				}
				writer := gzip.NewWriter(w)
				_, _ = writer.Write([]byte("decoded body"))
				_ = writer.Close()
			}))
			defer server.Close()
			defer unblock()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			ctx, recorder := codextickettrace.WithRecorder(ctx)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/path?token=private", nil)
			require.NoError(t, err)
			req.Header.Set("Accept-Encoding", "identity")
			upstream := NewHTTPUpstream(nil).(*httpUpstreamService)
			type result struct {
				response *http.Response
				err      error
			}
			done := make(chan result, 1)
			go func() {
				var response *http.Response
				var sendErr error
				if tlsEntry {
					response, sendErr = upstream.DoWithTLS(req, "", 41, 1, nil)
				} else {
					response, sendErr = upstream.Do(req, "", 41, 1)
				}
				done <- result{response, sendErr}
			}()
			require.Eventually(t, func() bool { s := recorder.Snapshot(); return s != nil && s.ResponseHeaderMS != nil }, time.Second, time.Millisecond)
			select {
			case <-done:
				t.Fatal("body decompression returned before gzip bytes were released")
			default:
			}
			snapshot := recorder.Snapshot()
			require.Equal(t, server.URL, snapshot.FinalOrigin)
			require.NotEmpty(t, snapshot.PeerAddr)
			unblock()
			var response result
			select {
			case response = <-done:
			case <-ctx.Done():
				t.Fatal("upstream did not finish")
			}
			require.NoError(t, response.err)
			body, err := io.ReadAll(response.response.Body)
			require.NoError(t, err)
			require.Equal(t, "decoded body", string(body))
			require.NoError(t, response.response.Body.Close())
			require.NoError(t, response.response.Body.Close())
			for _, entry := range upstream.clients {
				require.Equal(t, int64(0), atomic.LoadInt64(&entry.inFlight))
			}
			require.Equal(t, *snapshot.ResponseHeaderMS, *recorder.Snapshot().ResponseHeaderMS)
		})
	}
}
