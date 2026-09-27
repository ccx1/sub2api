package service

import (
	"context"
	"errors"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/util/transportdiag"
	"github.com/gin-gonic/gin"
)

type excelBPSTransportDiagnosticError struct {
	error
	trace *transportdiag.Trace
}

func (e *excelBPSTransportDiagnosticError) Unwrap() error { return e.error }

func (e *excelBPSWriteEvidence) observeResult(req *http.Request, resp *http.Response, err error) (*http.Response, error) {
	if resp != nil {
		if resp.Request == nil {
			resp.Request = req
		} else {
			resp.Request = resp.Request.WithContext(e.Trace.Context(resp.Request.Context()))
		}
		if resp.Body != nil {
			resp.Body = &excelBPSDiagnosticBody{ReadCloser: resp.Body, trace: &e.Trace}
		}
	}
	if err != nil {
		err = &excelBPSTransportDiagnosticError{error: err, trace: &e.Trace}
	}
	return resp, err
}

// 只观察阶段和返回原有错误链，不触发回退、代理评分或请求重放。
type excelBPSDiagnosticBody struct {
	io.ReadCloser
	trace *transportdiag.Trace
}

func (b *excelBPSDiagnosticBody) Read(p []byte) (int, error) {
	if len(p) > 0 {
		b.trace.MarkResponseBodyRead()
	}
	n, err := b.ReadCloser.Read(p)
	if err != nil && err != io.EOF {
		err = &excelBPSTransportDiagnosticError{error: err, trace: b.trace}
	}
	return n, err
}

func excelBPSTransportDiagnosticTrace(ctx context.Context, err error, responses []*http.Response) *transportdiag.Trace {
	var failure *excelBPSTransportDiagnosticError
	if errors.As(err, &failure) {
		return failure.trace
	}
	for _, resp := range responses {
		if resp != nil && resp.Request != nil {
			if trace := transportdiag.FromContext(resp.Request.Context()); trace != nil {
				return trace
			}
		}
	}
	return transportdiag.FromContext(ctx)
}

func excelBPSDiagnosticCanceled(ctx context.Context, c *gin.Context, _ error) bool {
	// 出站子 context 可以自行取消；只有入站请求终止才不记录出口故障。
	if c != nil && c.Request != nil {
		return c.Request.Context().Err() != nil
	}
	return ctx != nil && ctx.Err() != nil
}
