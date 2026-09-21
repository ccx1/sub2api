package service

import (
	"context"
	"sort"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type SharedPoolOverviewMetrics struct {
	TotalAccounts                     int64  `json:"total_accounts"`
	SchedulableAccounts               int64  `json:"schedulable_accounts"`
	ConcurrencyCapacity               int64  `json:"concurrency_capacity"`
	ConcurrencyUnlimited              bool   `json:"concurrency_unlimited"`
	CurrentConcurrency                *int64 `json:"current_concurrency"`
	ParticipatingAccounts             int64  `json:"participating_accounts"`
	ParticipatingConcurrency          int64  `json:"participating_concurrency"`
	ParticipatingConcurrencyUnlimited bool   `json:"participating_concurrency_unlimited"`
	capacity                          SharedPoolCapacity
	occupancy                         SharedPoolCapacity
}

type SharedPoolOverviewTier struct {
	SharedPoolOverviewMetrics
	Platform  string `json:"platform"`
	Tier      string `json:"tier"`
	Available bool   `json:"available"`
}

type SharedPoolOverview struct {
	SharedPoolOverviewMetrics
	SettlementMultiplier float64                  `json:"settlement_multiplier"`
	PlatformRateBPS      int                      `json:"platform_rate_bps"`
	ProxyRateBPS         int                      `json:"proxy_rate_bps"`
	UpdatedAt            time.Time                `json:"updated_at"`
	Tiers                []SharedPoolOverviewTier `json:"tiers"`
}

// 仓储仅返回汇总所需快照，不传递账号、用户或分组对象。
type SharedPoolOverviewAccount struct {
	AccountID            int64                            `json:"-"`
	Platform             string                           `json:"-"`
	Tier                 string                           `json:"-"`
	Available            bool                             `json:"-"`
	Participating        bool                             `json:"-"`
	Concurrency          int                              `json:"-"`
	UntrackedConcurrency bool                             `json:"-"`
	Ticket               *SharedPoolTicketAccountSnapshot `json:"-"`
}

type SharedPoolOverviewSource interface {
	SharedPoolOverviewAccounts(context.Context) ([]SharedPoolOverviewAccount, error)
}

func SharedPoolOverviewTierForAccount(account *Account) string {
	if account == nil {
		return "unknown"
	}
	if account.Type == AccountTypeAPIKey {
		return "api_key"
	}
	if tier := sharedAccountSubscriptionTier(account); tier != "" {
		return tier
	}
	return "unknown"
}

func (s *SharedPoolService) Overview(ctx context.Context, userID int64, cfg config.OpenAICodexTicketConfig) (*SharedPoolOverview, error) {
	// 分组仓储拥有代理健康读取依赖，优先复用生产目录的健康快照。
	source, ok := s.groups.(SharedPoolOverviewSource)
	if !ok {
		source, ok = s.repo.(SharedPoolOverviewSource)
	}
	if !ok {
		return nil, infraerrors.ServiceUnavailable("SHARED_OVERVIEW_UNAVAILABLE", "共享账号概览暂不可用")
	}
	settings, err := s.repo.SharedSettings(ctx)
	if err != nil {
		return nil, err
	}
	rates, err := s.repo.SharedUserRates(ctx)
	if err != nil {
		return nil, err
	}
	accounts, err := source.SharedPoolOverviewAccounts(ctx)
	if err != nil {
		return nil, err
	}
	result := buildSharedPoolOverview(accounts, cfg, time.Now())
	result.SettlementMultiplier = effectiveSharedSettlementMultiplier(settings, rates, userID)
	result.PlatformRateBPS, result.ProxyRateBPS = effectiveSharedRates(settings, rates, userID)
	return result, nil
}

func buildSharedPoolOverview(accounts []SharedPoolOverviewAccount, cfg config.OpenAICodexTicketConfig, now time.Time) *SharedPoolOverview {
	result := &SharedPoolOverview{UpdatedAt: now, Tiers: []SharedPoolOverviewTier{}}
	tiers, seen := make(map[[2]string]*SharedPoolOverviewTier), make(map[int64]bool)
	for _, account := range accounts {
		if seen[account.AccountID] {
			continue
		}
		seen[account.AccountID] = true
		if account.Tier == "" {
			account.Tier = "unknown"
		}
		key := [2]string{account.Platform, account.Tier}
		if tiers[key] == nil {
			tiers[key] = &SharedPoolOverviewTier{Platform: account.Platform, Tier: account.Tier}
		}
		result.SharedPoolOverviewMetrics.appendAccount(account)
		tiers[key].SharedPoolOverviewMetrics.appendAccount(account)
	}
	result.SharedPoolOverviewMetrics.finalize(cfg, now)
	for _, tier := range tiers {
		tier.SharedPoolOverviewMetrics.finalize(cfg, now)
		tier.Available = tier.SchedulableAccounts > 0
		result.Tiers = append(result.Tiers, *tier)
	}
	sort.Slice(result.Tiers, func(i, j int) bool {
		a, b := result.Tiers[i], result.Tiers[j]
		if a.Platform == b.Platform {
			return a.Tier < b.Tier
		}
		return a.Platform < b.Platform
	})
	return result
}

func (m *SharedPoolOverviewMetrics) appendAccount(account SharedPoolOverviewAccount) {
	appendSharedOverviewAccount(&m.capacity, account)
	if account.Participating {
		m.ParticipatingAccounts++
		if account.Concurrency <= 0 {
			m.ParticipatingConcurrencyUnlimited = true
		} else {
			m.ParticipatingConcurrency += int64(account.Concurrency)
		}
	}
	// 关闭调度或暂不可用后仍可能有请求占槽，当前并发不能按准入条件过滤。
	account.Available, account.Ticket = true, nil
	appendSharedOverviewAccount(&m.occupancy, account)
}

func appendSharedOverviewAccount(capacity *SharedPoolCapacity, account SharedPoolOverviewAccount) {
	capacity.TotalAccounts++
	if !account.Available {
		return
	}
	capacity.AvailableAccounts++
	capacity.AvailableAccountIDs = append(capacity.AvailableAccountIDs, account.AccountID)
	if account.UntrackedConcurrency {
		capacity.UntrackedConcurrencyAccountIDs = append(capacity.UntrackedConcurrencyAccountIDs, account.AccountID)
	}
	if account.Concurrency <= 0 {
		capacity.ConcurrencyUnlimited = true
		capacity.UnlimitedAccounts++
	} else {
		capacity.ConcurrencyCapacity += int64(account.Concurrency)
	}
	if account.Platform == PlatformOpenAI && account.Ticket != nil {
		ticket := *account.Ticket
		ticket.Available, ticket.Concurrency = true, account.Concurrency
		capacity.TicketAccounts = append(capacity.TicketAccounts, ticket)
	}
}

func (m *SharedPoolOverviewMetrics) finalize(cfg config.OpenAICodexTicketConfig, now time.Time) {
	m.capacity = GetSharedPoolCatalogCapacity(&m.capacity, cfg, now)
	m.TotalAccounts, m.SchedulableAccounts = m.capacity.TotalAccounts, m.capacity.AvailableAccounts
	m.ConcurrencyCapacity, m.ConcurrencyUnlimited = m.capacity.ConcurrencyCapacity, m.capacity.ConcurrencyUnlimited
	if m.TotalAccounts == 0 {
		m.CurrentConcurrency = new(int64)
	}
}

func (s *APIKeyService) EnrichSharedPoolOverviewConcurrency(ctx context.Context, overview *SharedPoolOverview, ticketCfg ...config.OpenAICodexTicketConfig) {
	if overview == nil {
		return
	}
	groups := []Group{{ID: 0, SharedPoolCapacity: &overview.occupancy}}
	for i := range overview.Tiers {
		groups = append(groups, Group{ID: int64(i + 1), SharedPoolCapacity: &overview.Tiers[i].occupancy})
	}
	var cfg config.OpenAICodexTicketConfig
	if len(ticketCfg) > 0 {
		cfg = ticketCfg[0]
	}
	counts := s.SharedPoolCurrentConcurrency(ctx, groups, cfg, overview.UpdatedAt)
	overview.CurrentConcurrency = nil
	if current, ok := counts[0]; ok {
		overview.CurrentConcurrency = &current
	}
	for i := range overview.Tiers {
		overview.Tiers[i].CurrentConcurrency = nil
		if current, ok := counts[int64(i+1)]; ok {
			overview.Tiers[i].CurrentConcurrency = &current
		}
	}
}
