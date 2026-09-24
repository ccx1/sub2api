package service

import (
	"fmt"
	"slices"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func validateCodexTicketProtection(cfg *config.OpenAICodexTicketConfig) error {
	if cfg.Protection == nil {
		return nil
	}
	policy := cfg.TicketProtection()
	cfg.Protection = &policy
	p := cfg.Protection
	checks := []struct {
		name            string
		value, min, max int
	}{
		{"proxy_silence_seconds", p.ProxySilenceSeconds, 1, 86400},
		{"max_pool_rounds", p.MaxPoolRounds, 1, 100},
		{"max_account_attempts", p.MaxAccountAttempts, 1, 1000},
		{"account_cooldown_seconds", p.AccountCooldownSeconds, 1, 86400},
		{"rejection_retry_interval_seconds", p.RejectionRetryIntervalSeconds, 1, 86400},
		{"rejection_retry_max_attempts", p.RejectionRetryMaxAttempts, 1, 1000},
		{"rejection_retry_cooldown_seconds", p.RejectionRetryCooldownSeconds, 1, 86400},
		{"proxy_ip_failure_account_threshold", p.ProxyIPFailureAccountThreshold, 1, 1000},
		{"proxy_ip_failure_window_seconds", p.ProxyIPFailureWindowSeconds, 1, 86400},
		{"proxy_ip_cooldown_seconds", p.ProxyIPCooldownSeconds, 1, 86400},
		{"proxy_ip_max_rounds", p.ProxyIPMaxRounds, 1, 100},
	}
	for _, check := range checks {
		if check.value < check.min || check.value > check.max {
			return invalidCodexTicketPolicy(fmt.Sprintf("protection.%s 必须在 %d 到 %d 之间", check.name, check.min, check.max))
		}
	}
	if len(p.RejectAndSilenceLengths) > 32 {
		return invalidCodexTicketPolicy("拒收并静默长度最多32项")
	}
	seen := make(map[int]bool)
	for _, length := range p.RejectAndSilenceLengths {
		if length < 16 || length > 8192 || seen[length] {
			return invalidCodexTicketPolicy("拒收并静默长度必须在16到8192之间且不能重复")
		}
		seen[length] = true
	}
	return validateCodexTicketProtectionTargets(*cfg)
}

func validateCodexTicketProtectionTargets(cfg config.OpenAICodexTicketConfig) error {
	p := cfg.Protection
	if p == nil || !p.Enabled || cfg.LengthMode == config.CodexTicketLengthAuto {
		return nil
	}
	if slices.Contains(p.RejectAndSilenceLengths, cfg.TargetLength) {
		return invalidCodexTicketPolicy("目标票长不能同时配置为拒收并静默长度")
	}
	for _, rule := range cfg.TierRules {
		if slices.Contains(p.RejectAndSilenceLengths, rule.TargetLength) {
			return invalidCodexTicketPolicy("套餐票长不能同时配置为拒收并静默长度")
		}
	}
	return nil
}
