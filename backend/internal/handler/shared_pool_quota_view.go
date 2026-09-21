package handler

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type sharedCodexTicketSettings interface {
	GetOpenAICodexTicketEnabled(context.Context, bool) bool
}

type sharedCodexTicketRuntimeSettings interface {
	GetOpenAICodexTicketRuntimeConfig(context.Context, config.OpenAICodexTicketConfig) config.OpenAICodexTicketConfig
}

var _ sharedCodexTicketRuntimeSettings = (*service.SettingService)(nil)

type sharedQuotaView struct {
	RateLimitResetCredits *service.OpenAIRateLimitResetCredits `json:"rate_limit_reset_credits,omitempty"`
	FetchedAt             int64                                `json:"fetched_at"`
}

func safeSharedResetCredits(credits *service.OpenAIRateLimitResetCredits) *service.OpenAIRateLimitResetCredits {
	if credits == nil {
		return nil
	}
	out := &service.OpenAIRateLimitResetCredits{AvailableCount: max(0, credits.AvailableCount)}
	for _, credit := range credits.Credits {
		if expires, err := time.Parse(time.RFC3339, credit.ExpiresAt); err == nil {
			out.Credits = append(out.Credits, service.OpenAIRateLimitResetCreditDetail{ExpiresAt: expires.Format(time.RFC3339)})
		}
	}
	return out
}

func sharedQuotaUsageView(usage *service.OpenAIQuotaUsage) *sharedQuotaView {
	if usage == nil {
		return nil
	}
	return &sharedQuotaView{RateLimitResetCredits: safeSharedResetCredits(usage.RateLimitResetCredits), FetchedAt: usage.FetchedAt}
}

func (h *SharedPoolHandler) enrichSharedUsageView(ctx context.Context, view *sharedAccountUsageView, account *service.Account) {
	if account == nil || !account.IsOpenAIOAuthLike() || account.IsShadow() {
		return
	}
	cfg := h.sharedTicketConfig(ctx)
	view.CodexTurnTickets = service.OpenAICodexTicketStatuses(account, cfg, time.Now())
	raw := account.Extra["codex_reset_credit_snapshot"]
	if raw == nil {
		return
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return
	}
	var credits service.OpenAIRateLimitResetCredits
	if json.Unmarshal(encoded, &credits) != nil {
		return
	}
	view.CodexResetCreditSnapshot = safeSharedResetCredits(&credits)
}

func (h *SharedPoolHandler) sharedTicketConfig(ctx context.Context) config.OpenAICodexTicketConfig {
	cfg := config.OpenAICodexTicketConfig{}
	if h.ticketConfig != nil {
		cfg = h.ticketConfig.Gateway.OpenAICodexTicket
	}
	cfg = config.NormalizeOpenAICodexTicketConfig(cfg)
	if h.ticketSettings != nil {
		settingsCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer cancel()
		if runtime, ok := h.ticketSettings.(sharedCodexTicketRuntimeSettings); ok {
			return runtime.GetOpenAICodexTicketRuntimeConfig(settingsCtx, cfg)
		}
		cfg.Enabled = h.ticketSettings.GetOpenAICodexTicketEnabled(settingsCtx, cfg.Enabled)
	}
	return cfg
}
