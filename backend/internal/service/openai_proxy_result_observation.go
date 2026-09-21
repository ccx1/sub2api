package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

func reportRandomProxyResult(ctx context.Context, account *Account, source any, successful bool) {
	if successful {
		ReportRandomProxySuccess(ctx, account, source)
		return
	}
	if ctx.Err() != nil || account == nil || !account.IsRandomProxy() || account.ProxyID == nil {
		return
	}
	reporter, ok := source.(randomProxyFailureReporter)
	if !ok {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 500*time.Millisecond)
	defer cancel()
	_ = reporter.ReportRandomProxyFailure(writeCtx, account.ID, *account.ProxyID)
}

type randomProxyReportedTransportError struct{ error }

func (e *randomProxyReportedTransportError) Unwrap() error { return e.error }

func observeRandomProxyHTTPResult(req *http.Request, account *Account, source any, resp *http.Response, err error) bool {
	if account == nil || !account.IsRandomProxy() {
		return false
	}
	if err != nil || resp == nil {
		return ReportRandomProxyTransportFailure(req.Context(), account, source, err)
	}
	RecordRandomProxyUsage(req.Context(), account, source)
	// 认证、限流和业务参数错误不是出口故障，不改变代理亲和。
	if resp.StatusCode == http.StatusProxyAuthRequired || resp.StatusCode >= 500 {
		reportRandomProxyResult(req.Context(), account, source, false)
		return false
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || resp.Body == nil {
		return false
	}
	receipt, _ := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	model := ""
	if receipt != nil {
		model = receipt.ticket.Model
	}
	returned := extractOpenAICodexTurnState(resp.Header)
	resp.Body = &randomProxyObservedHTTPBody{ReadCloser: resp.Body, ctx: req.Context(),
		account: cloneOpenAICodexTicketAccount(account), source: source,
		sse:        strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream"),
		checkModel: receipt != nil, invalidTicket: receipt != nil && len(returned) == 312 && strings.HasPrefix(returned, openAICodexTicketStatePrefix),
		observer: newOpenAICodexTicketResponseObserver(model)}
	return false
}

type randomProxyObservedHTTPBody struct {
	io.ReadCloser
	ctx                       context.Context
	account                   *Account
	source                    any
	sse                       bool
	checkModel, invalidTicket bool
	once                      sync.Once
	mu                        sync.Mutex
	observer                  *openAICodexTicketResponseObserver
}

func (b *randomProxyObservedHTTPBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.mu.Lock()
	b.observer.Observe(p[:n])
	if err == io.EOF {
		b.observer.Finish()
	}
	completed, matches := b.observer.Result()
	failed := b.observer.failed
	failureStatus := b.observer.failureStatus
	b.mu.Unlock()
	if (completed || failed || err != nil) && b.ctx.Err() == nil {
		b.once.Do(func() {
			if failureStatus >= 500 {
				reportRandomProxyResult(b.ctx, b.account, b.source, false)
			} else if failed {
				return
			} else if completed {
				reportRandomProxyResult(b.ctx, b.account, b.source, !b.invalidTicket && (!b.checkModel || matches))
			} else if err != io.EOF {
				ReportRandomProxyTransportFailure(b.ctx, b.account, b.source, err)
			} else if b.invalidTicket {
				reportRandomProxyResult(b.ctx, b.account, b.source, false)
			} else if !b.sse || !b.checkModel {
				reportRandomProxyResult(b.ctx, b.account, b.source, true)
			} else {
				ReportRandomProxyTransportFailure(b.ctx, b.account, b.source, io.ErrUnexpectedEOF)
			}
		})
	}
	return n, err
}
