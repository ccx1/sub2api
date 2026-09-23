package service

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type openAICodexTicketReceiptKey struct{}

type openAICodexTicketReceipt struct {
	account *Account
	ticket  openAICodexTicket
	config  config.OpenAICodexTicketConfig
	service *OpenAIGatewayService
}

func (s *OpenAIGatewayService) applyOpenAICodexTicketRequest(account *Account, model string, req *http.Request) error {
	receipt, err := s.applyOpenAICodexTicketSnapshot(req.Context(), account, model, req.Header)
	if err != nil {
		return err
	}
	if receipt != nil && receipt.ticket.usesCookies() && len(receipt.ticket.cookiesForURL(req.URL)) == 0 {
		receipt.ticket.clearHeaders(req.Header)
		if receipt.config.FailClosed {
			return ErrOpenAICodexTicketUnavailable
		}
		return nil
	}
	if receipt != nil {
		*req = *req.WithContext(context.WithValue(req.Context(), openAICodexTicketReceiptKey{}, receipt))
	}
	return nil
}

func (s *OpenAIGatewayService) validateOpenAICodexTicketSend(req *http.Request, account *Account) error {
	receipt, _ := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	if receipt == nil || !OpenAICodexTicketAccountEnabled(account) {
		return nil
	}
	cfg := s.openAICodexTicketConfigForAccount(req.Context(), account)
	if !cfg.FailClosed || !codexTicketConfigGatesModel(cfg, receipt.ticket.Model) {
		return nil
	}
	ticket := &receipt.ticket
	if !ticket.usable(time.Now(), account, cfg) ||
		s.codexTicketRevoked(openAICodexTicketKey(account.ID, ticket.Model), ticket) ||
		ticket.usesCookies() && len(ticket.cookiesForURL(req.URL)) == 0 {
		return ErrOpenAICodexTicketUnavailable
	}
	// 请求头组装与业务准入后再核验，并保持实际发送的凭据与快照一致。
	ticket.applyHeaders(req.Header)
	return nil
}

func (s *OpenAIGatewayService) observeOpenAICodexTicketResponse(req *http.Request, response *http.Response) {
	receipt, _ := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	if receipt == nil || response == nil || !receipt.ticket.matchesHeaders(req.Header) {
		return
	}
	var once sync.Once
	invalidate := func(detail *CodexTicketInvalidation) {
		once.Do(func() {
			// 响应透传不等待数据库；条件更新确保迟到回调不能删除新票。
			go s.invalidateOpenAICodexTicket(req.Context(), receipt.account, &receipt.ticket, detail)
		})
	}
	returned := extractOpenAICodexTurnState(response.Header)
	signals := codexTicketSignalsFromHeaders(response.Header)
	s.captureCodexTicketCookieCandidate(receipt, req, response)
	// Set-Cookie is a candidate credential update. Do not revoke the immutable
	// receipt used by this in-flight request; the scheduler must revalidate the
	// candidate before publishing it, while this request keeps its old snapshot.
	if response.StatusCode != http.StatusOK || response.Body == nil {
		return
	}
	if !receipt.ticket.usesCookies() && codexTicketStateRejected(returned, config.NormalizeOpenAICodexTicketConfig(receipt.config)) {
		detail := codexTicketRejectedInvalidation("http", returned)
		detail.Signals = cloneCodexTicketSignals(signals)
		invalidate(detail)
	}
	observer := newOpenAICodexTicketResponseObserver(receipt.ticket.Model)
	observer.diagnostics = &codexTicketResponseDiagnostics{wireStatus: response.StatusCode, signals: signals}
	response.Body = &openAICodexTicketObservedBody{ReadCloser: response.Body,
		observer: observer, mismatch: func(models []string, signals *CodexTicketSignals) {
			detail := newCodexTicketInvalidation("response_model_mismatch", "http", models)
			detail.Signals = signals
			invalidate(detail)
		}}
}

type openAICodexTicketObservedBody struct {
	io.ReadCloser
	mu       sync.Mutex
	observer *openAICodexTicketResponseObserver
	mismatch func([]string, *CodexTicketSignals)
}

func (b *openAICodexTicketObservedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.mu.Lock()
	b.observer.Observe(p[:n])
	if err == io.EOF {
		b.observer.Finish()
	}
	completed, matches := b.observer.Result()
	var models []string
	var signals *CodexTicketSignals
	if completed && !matches {
		models = append([]string(nil), b.observer.reportedModels...)
		if b.observer.diagnostics != nil {
			signals = cloneCodexTicketSignals(b.observer.diagnostics.signals)
		}
	}
	b.mu.Unlock()
	if completed && !matches {
		b.mismatch(models, signals)
	}
	return n, err
}

func (b *openAICodexTicketObservedBody) Close() error {
	err := b.ReadCloser.Close()
	b.mu.Lock()
	// 主动关闭可能来自客户端取消，不把尚未读取的尾部当成上游完成事件。
	b.observer.line, b.observer.event, b.observer.body = nil, nil, nil
	b.observer.finished = true
	b.mu.Unlock()
	return err
}
