package service

import (
	"context"
	"errors"
	"sync"
)

// HTTP 转 WS 断连后仍会排空上游，租约必须覆盖完整排空生命周期。
func (s *OpenAIGatewayService) beginCodexTicketBusinessDrainHold(ctx context.Context, account *Account) (context.Context, context.Context, func(), error) {
	if err := ctx.Err(); err != nil {
		return ctx, ctx, nil, err
	}
	holdCtx, release, err := s.beginCodexTicketBusinessHold(context.WithoutCancel(ctx), account)
	if err != nil {
		return ctx, ctx, nil, err
	}
	requestCtx, cleanup := codexTicketBusinessRequestContext(ctx, holdCtx)
	return requestCtx, holdCtx, func() { cleanup(); release() }, nil
}

func codexTicketBusinessRequestContext(ctx, holdCtx context.Context) (context.Context, func()) {
	requestCtx, cancel := context.WithCancelCause(ctx)
	stop := context.AfterFunc(holdCtx, func() { cancel(context.Cause(holdCtx)) })
	return requestCtx, func() { stop(); cancel(context.Canceled) }
}

// 透传的收发分属两条 goroutine；仅真实业务回合持有租约，空闲连接不占用。
type codexTicketBusinessTurnHold struct {
	ctx     context.Context
	cancel  context.CancelFunc
	service *OpenAIGatewayService
	account *Account
	mu      sync.Mutex
	release func()
	stop    func() bool
}

func (h *codexTicketBusinessTurnHold) begin() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.release != nil {
		return errors.New("codex ticket business turn already active")
	}
	ctx, release, err := h.service.beginCodexTicketBusinessHold(h.ctx, h.account)
	if err != nil {
		return err
	}
	h.release = release
	h.stop = context.AfterFunc(ctx, func() {
		if errors.Is(context.Cause(ctx), ErrCodexTicketBusinessHoldLost) {
			h.cancel()
		}
	})
	return nil
}

func (h *codexTicketBusinessTurnHold) finish() {
	h.mu.Lock()
	release, stop := h.release, h.stop
	h.release, h.stop = nil, nil
	h.mu.Unlock()
	if stop != nil {
		stop()
	}
	if release != nil {
		release()
	}
}

func (h *codexTicketBusinessTurnHold) close() {
	h.finish()
	h.cancel()
}
