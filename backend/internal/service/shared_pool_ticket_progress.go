package service

import (
	"slices"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// 只保留判断就绪所需的元数据，不将账号、凭证或票据正文带出仓储汇总。
type SharedPoolTicketAccountSnapshot struct {
	AccountID        int64
	Available        bool
	Concurrency      int
	subscriptionTier string
	tickets          map[string]sharedPoolTicketMetadata
}

type sharedPoolTicketMetadata struct {
	length    int
	expiresAt time.Time
	verified  bool
	autoReady bool
}

func NewSharedPoolTicketAccountSnapshot(account *Account, now time.Time) *SharedPoolTicketAccountSnapshot {
	if account == nil || account.Status != StatusActive || !OpenAICodexTicketAccountEnabled(account) {
		return nil
	}
	snapshot := &SharedPoolTicketAccountSnapshot{AccountID: account.ID, subscriptionTier: openAICodexTicketSubscriptionTier(account), tickets: make(map[string]sharedPoolTicketMetadata)}
	for key, raw := range account.Extra {
		if !strings.HasPrefix(key, openAICodexTicketExtraKeyPrefix) {
			continue
		}
		model := normalizeOpenAICodexTicketModel(strings.TrimPrefix(key, openAICodexTicketExtraKeyPrefix))
		if key != openAICodexTicketExtraKey(model) {
			continue
		}
		ticket := parseOpenAICodexTicketFromAny(account.ID, model, raw)
		// 此处验证票据形状，目标长度留给请求使用的运行配置判断。
		if model != "" && ticket != nil && ticket.valid(now, ticket.Length) && ticket.accountCompatible(account) {
			snapshot.tickets[model] = sharedPoolTicketMetadata{length: ticket.Length, expiresAt: ticket.ExpiresAt,
				verified: ticket.Verified, autoReady: ticket.autoUsable(now, account)}
		}
	}
	return snapshot
}

// 展示打票目标模型的可用容量，不改写调度状态或各模型的真实门控。
func GetSharedPoolCatalogCapacity(capacity *SharedPoolCapacity, cfg config.OpenAICodexTicketConfig, now time.Time) SharedPoolCapacity {
	if capacity == nil {
		return SharedPoolCapacity{}
	}
	result := *capacity
	if !cfg.Enabled || !cfg.FailClosed {
		return result
	}
	models := sharedPoolTicketModels(cfg.Models)
	if len(models) == 0 {
		return result
	}
	cfg = config.NormalizeOpenAICodexTicketConfig(cfg)
	excluded := make(map[int64]bool)
	for _, account := range capacity.TicketAccounts {
		if !account.Available || account.hasReadyModelForConfig(models, cfg, now) {
			continue
		}
		excluded[account.AccountID] = true
		if result.AvailableAccounts > 0 {
			result.AvailableAccounts--
		}
		if account.Concurrency <= 0 {
			if result.UnlimitedAccounts > 0 {
				result.UnlimitedAccounts--
			}
			result.ConcurrencyUnlimited = result.UnlimitedAccounts > 0
		} else {
			result.ConcurrencyCapacity -= int64(account.Concurrency)
			if result.ConcurrencyCapacity < 0 {
				result.ConcurrencyCapacity = 0
			}
		}
	}
	if len(excluded) > 0 && capacity.AvailableAccountIDs != nil {
		result.AvailableAccountIDs = make([]int64, 0, len(capacity.AvailableAccountIDs))
		for _, id := range capacity.AvailableAccountIDs {
			if !excluded[id] {
				result.AvailableAccountIDs = append(result.AvailableAccountIDs, id)
			}
		}
	}
	return result
}

func (a SharedPoolTicketAccountSnapshot) hasReadyModelForConfig(models []string, cfg config.OpenAICodexTicketConfig, now time.Time) bool {
	if cfg.LengthMode != config.CodexTicketLengthAuto {
		targetLength := codexTicketTierTargetLength(a.subscriptionTier, cfg)
		return !slices.Contains(cfg.RejectedLengths, targetLength) && a.hasReadyModel(models, targetLength, now)
	}
	for _, model := range models {
		if ticket, exists := a.tickets[model]; exists && ticket.verified && ticket.autoReady && now.Before(ticket.expiresAt) {
			return true
		}
	}
	return false
}

func (a SharedPoolTicketAccountSnapshot) hasReadyModel(models []string, targetLength int, now time.Time) bool {
	for _, model := range models {
		if ticket, exists := a.tickets[model]; exists && ticket.length == targetLength && now.Before(ticket.expiresAt) {
			return true
		}
	}
	return false
}

func sharedPoolTicketModels(models []string) []string {
	if len(models) == 0 {
		return []string{openAICodexTicketDefaultModel, openAICodexTicketDefaultSolModel}
	}
	out := make([]string, 0, len(models))
	for _, model := range models {
		if model = normalizeOpenAICodexTicketModel(model); model != "" {
			out = append(out, model)
		}
	}
	return out
}
