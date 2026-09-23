package service

import (
	"context"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func normalizeCodexTicketTier(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '-' || r == '_' {
			return -1
		}
		return unicode.ToLower(r)
	}, value)
}

// 显式套餐元数据优先；未知非空套餐必须使用兜底规则，不能误归类为个人 Pro。
func openAICodexTicketSubscriptionTier(account *Account) string {
	if account == nil {
		return ""
	}
	for _, key := range []string{"plan_type", "chatgpt_plan_type", "subscription_tier"} {
		if tier := normalizeCodexTicketTier(account.GetCredential(key)); tier != "" {
			return tier
		}
	}
	for _, key := range []string{"id_token", "access_token"} {
		claims := sharedOpenAIPlanClaims(account.GetCredential(key))
		if claims == nil {
			continue
		}
		if id := account.GetChatGPTAccountID(); id != "" && claims.ChatGPTAccountID != "" && id != claims.ChatGPTAccountID {
			continue
		}
		if tier := normalizeCodexTicketTier(claims.ChatGPTPlanType); tier != "" {
			return tier
		}
	}
	return ""
}

func codexTicketTierTargetLength(tier string, cfg config.OpenAICodexTicketConfig) int {
	cfg = config.NormalizeOpenAICodexTicketConfig(cfg)
	for _, rule := range cfg.TierRules {
		if tier != "" && normalizeCodexTicketTier(rule.Tier) == tier {
			return rule.TargetLength
		}
		for _, alias := range rule.Aliases {
			if tier != "" && normalizeCodexTicketTier(alias) == tier {
				return rule.TargetLength
			}
		}
	}
	return cfg.TargetLength
}

func openAICodexTicketTargetLength(account *Account, cfg config.OpenAICodexTicketConfig) int {
	return codexTicketTierTargetLength(openAICodexTicketSubscriptionTier(account), cfg)
}

func codexTicketStateRejected(state string, cfg config.OpenAICodexTicketConfig) bool {
	if config.CodexTicketUsesCookies(cfg) || cfg.LengthMode == config.CodexTicketLengthAuto {
		return false
	}
	return strings.HasPrefix(state, openAICodexTicketStatePrefix) && slices.Contains(cfg.RejectedLengths, len(state))
}

func codexTicketConfigGatesModel(cfg config.OpenAICodexTicketConfig, model string) bool {
	model = normalizeOpenAICodexTicketModel(model)
	if !cfg.Enabled || model == "" {
		return false
	}
	for _, candidate := range cfg.Models {
		if normalizeOpenAICodexTicketModel(candidate) == model {
			return true
		}
	}
	return false
}

func (ticket *openAICodexTicket) usable(now time.Time, account *Account, cfg config.OpenAICodexTicketConfig) bool {
	cfg = resolveCodexTicketCredentialConfig(account, cfg)
	if ticket == nil || !ticket.accountCompatible(account) ||
		(ticket.VerificationSkipped && config.CodexTicketBusinessVerificationEnabled(cfg)) {
		return false
	}
	if config.CodexTicketUsesCookies(cfg) {
		return ticket.cookieUsable(now, cfg)
	}
	if ticket.usesCookies() {
		return false
	}
	if cfg.LengthMode == config.CodexTicketLengthAuto {
		return ticket.autoUsable(now, account)
	}
	return ticket.valid(now, openAICodexTicketTargetLength(account, cfg)) && !codexTicketStateRejected(ticket.State, cfg)
}

func (s *OpenAIGatewayService) openAICodexTicketProbeConfigCurrent(ctx context.Context, input openAICodexTicketProbeInput) bool {
	if input.Config == nil {
		return true
	}
	current, captured := s.openAICodexTicketConfigForAccount(ctx, input.Account), resolveCodexTicketCredentialConfig(input.Account, *input.Config)
	// 总开关由 controls 单独检查，配置快照只用于阻止旧规则继续出站及发布。
	current.Enabled, captured.Enabled = false, false
	current.HarvestProxyURL, captured.HarvestProxyURL = "", ""
	return reflect.DeepEqual(current, captured) && input.SubscriptionTier == openAICodexTicketSubscriptionTier(input.Account)
}
