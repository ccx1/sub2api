package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/Wei-Shaw/sub2api/internal/util/transportdiag"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type excelBPSPinnedEgressKey struct{}

// Missing trace is not evidence of safety. Preserve positive evidence from
// body reads as well, including transports that omit net/http write callbacks.
type excelBPSWriteEvidence struct {
	started      atomic.Bool
	handedToHTTP atomic.Bool
}

func (e *excelBPSWriteEvidence) request(req *http.Request) *http.Request {
	mark := func() { e.handedToHTTP.Store(true) }
	trace := &httptrace.ClientTrace{
		GetConn:              func(string) { e.started.Store(true) },
		GotConn:              func(httptrace.GotConnInfo) { mark() },
		WroteHeaderField:     func(string, []string) { mark() },
		WroteHeaders:         mark,
		WroteRequest:         func(httptrace.WroteRequestInfo) { mark() },
		GotFirstResponseByte: mark,
	}
	req = req.Clone(httptrace.WithClientTrace(req.Context(), trace))
	if req.Body != nil {
		req.Body = &excelBPSTrackedBody{ReadCloser: req.Body, mark: mark}
	}
	if getBody := req.GetBody; getBody != nil {
		req.GetBody = func() (io.ReadCloser, error) {
			// Existing native fallback asks GetBody before switching egress. A read
			// without trace callbacks must still veto replay of this POST.
			if e.handedToHTTP.Load() {
				return nil, errors.New("Excel BPS request may already have been sent")
			}
			body, err := getBody()
			if err != nil {
				return nil, err
			}
			return &excelBPSTrackedBody{ReadCloser: body, mark: mark}, nil
		}
	}
	return req
}

func (e *excelBPSWriteEvidence) unsent() bool { return e.started.Load() && !e.handedToHTTP.Load() }

type excelBPSTrackedBody struct {
	io.ReadCloser
	mark func()
}

func (b *excelBPSTrackedBody) Read(p []byte) (int, error) {
	if len(p) > 0 {
		b.mark()
	}
	return b.ReadCloser.Read(p)
}

// Account proxy selection has already run before this bridge. Preserve the
// native random/fixed proxy, TLS and bounded fallback policies for every send.
func (s *OpenAIGatewayService) doExcelBPSRequest(ctx context.Context, c *gin.Context, account *Account, scope string, body []byte, token, accountID string) (*http.Response, string, error) {
	proxy := ""
	if account.Proxy != nil {
		proxy = account.Proxy.URL()
	}
	req, err := newExcelBPSRequest(ctx, body, token, accountID)
	if err != nil {
		return nil, proxy, err
	}
	c.Set("excel_bps_upstream_attempt", 1)
	resp, err := s.doExcelBPSUpstream(req, proxy, account)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		recordExcelBPSTransportFailure(ctx, c, account, scope, proxy, err, "transport", 1, false)
		return nil, proxy, err
	}
	return resp, proxy, nil
}

func recordExcelBPSTransportFailure(ctx context.Context, c *gin.Context, account *Account, scope, _ string, err error, stage string, attempt int, retry bool) {
	kind := transportdiag.Classify(err)
	digest := sha256.Sum256([]byte(scope))
	sessionHash := hex.EncodeToString(digest[:8])
	detail, _ := json.Marshal(map[string]any{
		"error_kind": kind, "error_type": fmt.Sprintf("%T", err),
		"session_hash": sessionHash, "attempt": attempt, "retry_before_send": retry,
	})
	message := "Excel BPS " + stage + " failed: " + kind
	if !retry {
		setOpsUpstreamError(c, 0, message, string(detail))
	}
	proxyID, proxyName := runtimeProxyErrorAttribution(account, err)
	appendOpsUpstreamError(c, OpsUpstreamErrorEvent{
		Platform: account.Platform, AccountID: account.ID, AccountName: account.Name,
		ProxyID: proxyID, ProxyName: proxyName,
		UpstreamURL: basispoints.ResponsesURL, Kind: "request_error", Stage: stage,
		Scope: "excel_bps", Reason: kind, Message: message, Detail: string(detail),
	})
	logger.FromContext(ctx).Warn("excel_bps.transport_failed",
		zap.Int64("account_id", account.ID), zap.String("stage", stage),
		zap.String("error_kind", kind), zap.String("error_type", fmt.Sprintf("%T", err)),
		zap.String("session_hash", sessionHash), zap.Int("attempt", attempt),
		zap.Bool("retry_before_send", retry))
}
