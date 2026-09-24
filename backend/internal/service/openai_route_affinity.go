package service

import (
	"context"
	"log/slog"
	"time"
)

const openAIWSRouteAffinityUnavailableReason = "route_affinity_unavailable"

// pauseOpenAIModelForRouteAffinity applies the same scheduler effect as a
// temporarily invalid ticket, while keeping the ticket itself available for
// background refresh and route probing.
func (s *OpenAIGatewayService) pauseOpenAIModelForRouteAffinity(ctx context.Context, account *Account, model string, cooldownSeconds int) {
	if s == nil || s.accountRepo == nil || account == nil || account.ID <= 0 {
		return
	}
	model = normalizeOpenAICodexTicketModel(model)
	if model == "" {
		return
	}
	if cooldownSeconds < 30 {
		cooldownSeconds = 120
	}
	if cooldownSeconds > 900 {
		cooldownSeconds = 900
	}
	now := time.Now()
	until := now.Add(time.Duration(cooldownSeconds) * time.Second)
	setAccountModelRateLimitSnapshot(account, model, until, openAIWSRouteAffinityUnavailableReason, now)
	if ctx == nil {
		ctx = context.Background()
	}
	stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.accountRepo.SetModelRateLimit(
		stateCtx,
		account.ID,
		model,
		until,
		openAIWSRouteAffinityUnavailableReason,
	); err != nil {
		slog.Warn("openai_route_affinity_cooldown_failed", "account_id", account.ID, "model", model, "error", err)
	}
}
