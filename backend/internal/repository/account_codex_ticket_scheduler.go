package repository

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.CodexTicketScheduler = (*accountRepository)(nil)
var _ service.CodexTicketSessionEpochReader = (*accountRepository)(nil)

func (r *accountRepository) GetCodexTicketSessionEpoch(ctx context.Context, scope service.CodexTicketSessionScope) (string, error) {
	return r.proxyPool.GetCodexTicketSessionEpoch(ctx, scope)
}

func (r *accountRepository) ReserveCodexTicket(ctx context.Context, req service.CodexTicketReserveRequest) (*service.CodexTicketReservation, error) {
	if r.proxyPool == nil {
		return nil, errors.New("codex ticket scheduler unavailable")
	}
	return r.proxyPool.ReserveCodexTicket(ctx, req)
}
func (r *accountRepository) StartCodexTicket(ctx context.Context, reservation *service.CodexTicketReservation) error {
	return r.proxyPool.StartCodexTicket(ctx, reservation)
}
func (r *accountRepository) CheckCodexTicketStage(ctx context.Context, reservation *service.CodexTicketReservation, proxy *service.Proxy) error {
	return r.proxyPool.CheckCodexTicketStage(ctx, reservation, proxy)
}
func (r *accountRepository) ValidateCodexTicket(ctx context.Context, reservation *service.CodexTicketReservation) error {
	return r.proxyPool.ValidateCodexTicket(ctx, reservation)
}
func (r *accountRepository) ReportCodexTicketHarvest(ctx context.Context, reservation *service.CodexTicketReservation, accepted, silence, neutral bool) error {
	return r.proxyPool.ReportCodexTicketHarvest(ctx, reservation, accepted, silence, neutral)
}
func (r *accountRepository) FinishCodexTicket(ctx context.Context, req service.CodexTicketFinishRequest) error {
	return r.proxyPool.FinishCodexTicket(ctx, req)
}
func (r *accountRepository) GetCodexTicketRuntimeStatus(ctx context.Context, id int64) (*service.CodexTicketRuntimeStatus, error) {
	return r.proxyPool.GetCodexTicketRuntimeStatus(ctx, id)
}
