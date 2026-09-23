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
	credentialMode   string
	tickets          map[string]sharedPoolTicketMetadata
}

type sharedPoolTicketMetadata struct {
	length              int
	expiresAt           time.Time
	credentialMode      string
	cookieReady         bool
	verified            bool
	verificationSkipped bool
	autoReady           bool
	standby             *sharedPoolTicketMetadata
}

func NewSharedPoolTicketAccountSnapshot(account *Account, now time.Time) *SharedPoolTicketAccountSnapshot {
	if account == nil || account.Status != StatusActive || !OpenAICodexTicketAccountEnabled(account) {
		return nil
	}
	snapshot := &SharedPoolTicketAccountSnapshot{AccountID: account.ID, subscriptionTier: openAICodexTicketSubscriptionTier(account), tickets: make(map[string]sharedPoolTicketMetadata)}
	if policy, err := parseCodexTicketCredentialPolicy(account.Extra[CodexTicketCredentialPolicyExtraKey]); err == nil {
		snapshot.credentialMode = policy.Mode
	}
	for key, raw := range account.Extra {
		if !strings.HasPrefix(key, openAICodexTicketExtraKeyPrefix) {
			continue
		}
		model := normalizeOpenAICodexTicketModel(strings.TrimPrefix(key, openAICodexTicketExtraKeyPrefix))
		if key != openAICodexTicketExtraKey(model) {
			continue
		}
		inventory := parseOpenAICodexTicketFromAny(account.ID, model, raw)
		var metadata, tail *sharedPoolTicketMetadata
		for _, ticket := range codexTicketSlots(inventory) {
			// 保留完整库存的脱敏元数据，目标长度与复核开关由展示时的运行配置判断。
			candidate := sharedPoolTicketMetadataFor(account, ticket, now)
			if model == "" || candidate == nil {
				continue
			}
			if metadata == nil {
				metadata = candidate
			} else {
				tail.standby = candidate
			}
			tail = candidate
		}
		if metadata != nil {
			snapshot.tickets[model] = *metadata
		}
	}
	return snapshot
}

func sharedPoolTicketMetadataFor(account *Account, ticket *openAICodexTicket, now time.Time) *sharedPoolTicketMetadata {
	if ticket == nil || ticket.Revoked || !ticket.accountCompatible(account) {
		return nil
	}
	mode := ticket.CredentialMode
	if mode == "" {
		mode = config.CodexTicketCredentialState
	}
	cookieReady := ticket.usesCookies() && ticket.cookieUsable(now, config.OpenAICodexTicketConfig{CredentialMode: mode})
	if ticket.usesCookies() && !cookieReady || !ticket.usesCookies() && !ticket.valid(now, ticket.Length) {
		return nil
	}
	return &sharedPoolTicketMetadata{length: ticket.Length, expiresAt: ticket.hardExpiresAt(), credentialMode: mode,
		cookieReady: cookieReady, verified: ticket.Verified, verificationSkipped: ticket.VerificationSkipped,
		autoReady: ticket.autoUsable(now, account)}
}

// 参与调度的统计要求持有有效票，独立于路由允许无票请求的 FailClosed 策略。
func (a *SharedPoolTicketAccountSnapshot) hasReadyModelForParticipation(cfg config.OpenAICodexTicketConfig, now time.Time) bool {
	if a == nil || !cfg.Enabled {
		return false
	}
	cfg = config.NormalizeOpenAICodexTicketConfig(cfg)
	return a.hasReadyModelForConfig(sharedPoolTicketModels(cfg.Models), cfg, now)
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
	cfg = config.NormalizeCodexTicketCredentialConfig(cfg)
	if a.credentialMode != "" && a.credentialMode != "inherit" {
		cfg.CredentialMode = a.credentialMode
	}
	targetLength := codexTicketTierTargetLength(a.subscriptionTier, cfg)
	for _, model := range models {
		ticket, exists := a.tickets[model]
		if !exists {
			continue
		}
		for slot := &ticket; slot != nil; slot = slot.standby {
			if slot.readyForConfig(cfg, targetLength, now) {
				return true
			}
		}
	}
	return false
}

func (ticket *sharedPoolTicketMetadata) readyForConfig(cfg config.OpenAICodexTicketConfig, targetLength int, now time.Time) bool {
	if ticket == nil || !now.Before(ticket.expiresAt) ||
		(ticket.verificationSkipped && config.CodexTicketBusinessVerificationEnabled(cfg)) {
		return false
	}
	mode := ticket.credentialMode
	if mode == "" {
		mode = config.CodexTicketCredentialState
	}
	if mode != cfg.CredentialMode {
		return false
	}
	if config.CodexTicketUsesCookies(cfg) {
		return ticket.cookieReady
	}
	if cfg.LengthMode == config.CodexTicketLengthAuto {
		return (ticket.verified || ticket.verificationSkipped) && ticket.autoReady
	}
	return ticket.length == targetLength && !slices.Contains(cfg.RejectedLengths, targetLength)
}

func (a SharedPoolTicketAccountSnapshot) hasReadyModel(models []string, targetLength int, now time.Time) bool {
	for _, model := range models {
		ticket, exists := a.tickets[model]
		if !exists {
			continue
		}
		for slot := &ticket; slot != nil; slot = slot.standby {
			if slot.length == targetLength && now.Before(slot.expiresAt) {
				return true
			}
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
