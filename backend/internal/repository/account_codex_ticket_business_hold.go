package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.CodexTicketBusinessHoldStore = (*accountRepository)(nil)

func (r *accountRepository) AcquireCodexTicketBusinessHold(ctx context.Context, lease service.CodexTicketBusinessLease) error {
	return r.proxyPool.AcquireCodexTicketBusinessHold(ctx, lease)
}

func (r *accountRepository) RenewCodexTicketBusinessHold(ctx context.Context, lease service.CodexTicketBusinessLease) error {
	return r.proxyPool.RenewCodexTicketBusinessHold(ctx, lease)
}

func (r *accountRepository) ReleaseCodexTicketBusinessHold(ctx context.Context, lease service.CodexTicketBusinessLease) error {
	return r.proxyPool.ReleaseCodexTicketBusinessHold(ctx, lease)
}

func (r *accountRepository) CodexTicketBusinessHoldUntil(ctx context.Context, accountID int64) (time.Time, error) {
	return r.proxyPool.CodexTicketBusinessHoldUntil(ctx, accountID)
}
