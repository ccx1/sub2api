package handler

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type sharedPoolCatalogView struct {
	ID                   int64   `json:"id"`
	Name                 string  `json:"name"`
	Description          string  `json:"description"`
	Platform             string  `json:"platform"`
	RateMultiplier       float64 `json:"rate_multiplier"`
	AvailableAccounts    int64   `json:"available_accounts"`
	TotalAccounts        *int64  `json:"total_accounts"`
	ConcurrencyCapacity  *int64  `json:"concurrency_capacity"`
	ConcurrencyUnlimited bool    `json:"concurrency_unlimited"`
	CurrentConcurrency   *int64  `json:"current_concurrency"`
}

func newSharedPoolCatalogView(group service.Group, cfg config.OpenAICodexTicketConfig, now time.Time) sharedPoolCatalogView {
	view := sharedPoolCatalogView{ID: group.ID, Name: group.Name, Description: group.Description,
		Platform: group.Platform, RateMultiplier: group.RateMultiplier, AvailableAccounts: group.ActiveAccountCount}
	if capacity := group.SharedPoolCapacity; capacity != nil {
		stats := *capacity
		if group.Platform == service.PlatformOpenAI {
			stats = service.GetSharedPoolCatalogCapacity(capacity, cfg, now)
		}
		view.AvailableAccounts = stats.AvailableAccounts
		view.TotalAccounts = &stats.TotalAccounts
		view.ConcurrencyCapacity = &stats.ConcurrencyCapacity
		view.ConcurrencyUnlimited = stats.ConcurrencyUnlimited
	}
	return view
}
