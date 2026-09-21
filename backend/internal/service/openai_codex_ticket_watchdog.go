package service

import (
	"context"
	"io"
	"net/http"
	"sync"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type openAICodexTicketReceiptKey struct{}

type openAICodexTicketReceipt struct {
	account *Account
	ticket  openAICodexTicket
	config  config.OpenAICodexTicketConfig
}

func (s *OpenAIGatewayService) applyOpenAICodexTicketRequest(account *Account, model string, req *http.Request) error {
	receipt, err := s.applyOpenAICodexTicketSnapshot(req.Context(), account, model, req.Header)
	if err != nil {
		return err
	}
	if receipt != nil {
		*req = *req.WithContext(context.WithValue(req.Context(), openAICodexTicketReceiptKey{}, receipt))
	}
	return nil
}

func (s *OpenAIGatewayService) observeOpenAICodexTicketResponse(req *http.Request, response *http.Response) {
	receipt, _ := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	if receipt == nil || response == nil || response.StatusCode != http.StatusOK || response.Body == nil ||
		req.Header.Get(openAICodexTurnStateHeader) != receipt.ticket.State {
		return
	}
	var once sync.Once
	invalidate := func() {
		once.Do(func() {
			// 响应透传不等待数据库；条件更新确保迟到回调不能删除新票。
			go s.invalidateOpenAICodexTicket(req.Context(), receipt.account, &receipt.ticket)
		})
	}
	returned := extractOpenAICodexTurnState(response.Header)
	if codexTicketStateRejected(returned, config.NormalizeOpenAICodexTicketConfig(receipt.config)) {
		invalidate()
	}
	response.Body = &openAICodexTicketObservedBody{ReadCloser: response.Body,
		observer: newOpenAICodexTicketResponseObserver(receipt.ticket.Model), mismatch: invalidate}
}

type openAICodexTicketObservedBody struct {
	io.ReadCloser
	mu       sync.Mutex
	observer *openAICodexTicketResponseObserver
	mismatch func()
}

func (b *openAICodexTicketObservedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.mu.Lock()
	b.observer.Observe(p[:n])
	if err == io.EOF {
		b.observer.Finish()
	}
	completed, matches := b.observer.Result()
	b.mu.Unlock()
	if completed && !matches {
		b.mismatch()
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
