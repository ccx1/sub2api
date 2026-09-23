package service

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var codexTicketTierNamePattern = regexp.MustCompile(`^[a-zA-Z0-9 _-]{1,80}$`)

func validateCodexTicketPolicy(cfg *config.OpenAICodexTicketConfig) error {
	if err := config.ValidateCodexTicketCredentialConfig(cfg); err != nil {
		return invalidCodexTicketPolicy(err.Error())
	}
	if err := config.ValidateCodexTicketRefreshStrategy(cfg); err != nil {
		return invalidCodexTicketPolicy(err.Error())
	}
	if cfg.PoolCapacity == 0 {
		cfg.PoolCapacity = config.DefaultCodexTicketPoolCapacity
	}
	if cfg.VerifyBusiness == nil {
		enabled := true
		cfg.VerifyBusiness = &enabled
	}
	if cfg.BusinessVerificationRounds == 0 {
		cfg.BusinessVerificationRounds = 1
	}
	if cfg.SessionMode == "" {
		cfg.SessionMode = config.CodexTicketSessionRandom
	}
	switch cfg.SessionMode {
	case config.CodexTicketSessionRandom, config.CodexTicketSessionAccount, config.CodexTicketSessionAccountModel:
	default:
		return invalidCodexTicketPolicy("打票 Session 模式必须是 random、account 或 account_model")
	}
	if cfg.ProxyFailureThreshold == 0 {
		cfg.ProxyFailureThreshold = 3
	}
	if cfg.ProxyFailureThreshold < 1 || cfg.ProxyFailureThreshold > 1000 {
		return invalidCodexTicketPolicy("打票代理连续失败换绑阈值必须在 1 到 1000 之间")
	}
	if err := validateCodexTicketProtection(cfg); err != nil {
		return err
	}
	if cfg.LengthMode == "" {
		cfg.LengthMode = config.CodexTicketLengthStrict
	}
	if cfg.LengthMode != config.CodexTicketLengthStrict && cfg.LengthMode != config.CodexTicketLengthAuto {
		return invalidCodexTicketPolicy("票据长度模式必须是 strict 或 auto")
	}
	checks := []struct {
		name            string
		value, min, max int
	}{
		{"target_length", cfg.TargetLength, 16, 8192},
		{"ttl_seconds", cfg.TTLSeconds, 60, 86400},
		{"pool_capacity", cfg.PoolCapacity, 1, config.MaxCodexTicketPoolCapacity},
		{"refresh_before_seconds", cfg.RefreshBeforeSeconds, 0, cfg.TTLSeconds - 1},
		{"harvest_probe_interval_seconds", cfg.HarvestProbeIntervalSeconds, 1, 3600},
		{"harvest_attempt_timeout_seconds", cfg.HarvestAttemptTimeoutSeconds, 1, 300},
		{"business_verification_rounds", cfg.BusinessVerificationRounds, 1, 10},
		{"harvest_concurrency", cfg.HarvestConcurrency, 1, 64},
		{"retry_max_attempts", cfg.RetryMaxAttempts, 0, 1000},
		{"retry_exhausted_cooldown_seconds", cfg.RetryExhaustedCooldownSeconds, 1, 86400},
		{"auth_cooldown_seconds", cfg.AuthCooldownSeconds, 1, 86400},
		{"rate_limit_cooldown_seconds", cfg.RateLimitCooldownSeconds, 1, 86400},
	}
	for _, item := range checks {
		if item.value < item.min || item.value > item.max {
			return invalidCodexTicketPolicy(fmt.Sprintf("%s 必须在 %d 到 %d 之间", item.name, item.min, item.max))
		}
	}
	if len(cfg.RetryBackoffSeconds) < 1 || len(cfg.RetryBackoffSeconds) > 16 {
		return invalidCodexTicketPolicy("重试退避需要 1 到 16 个间隔")
	}
	previous := 0
	for _, seconds := range cfg.RetryBackoffSeconds {
		if seconds < 1 || seconds > 86400 || seconds < previous {
			return invalidCodexTicketPolicy("重试退避间隔必须递增或相等，范围为 1 到 86400 秒")
		}
		previous = seconds
	}
	if err := validateCodexTicketModels(cfg); err != nil {
		return err
	}
	return validateCodexTicketTiers(cfg)
}

func validateCodexTicketModels(cfg *config.OpenAICodexTicketConfig) error {
	if len(cfg.Models) < 1 || len(cfg.Models) > 32 {
		return invalidCodexTicketPolicy("请配置 1 到 32 个模型")
	}
	seen := make(map[string]bool)
	for i, model := range cfg.Models {
		model = strings.TrimSpace(model)
		if model == "" || len(model) > 160 || strings.ContainsAny(model, " \t\r\n\x00") || seen[model] {
			return invalidCodexTicketPolicy("模型名称不能为空、重复或包含空白字符")
		}
		seen[model], cfg.Models[i] = true, model
	}
	return nil
}

func validateCodexTicketTiers(cfg *config.OpenAICodexTicketConfig) error {
	if len(cfg.TierRules) > 32 || len(cfg.RejectedLengths) > 32 {
		return invalidCodexTicketPolicy("套餐规则和拒绝长度最多各 32 项")
	}
	rejected := make(map[int]bool)
	for _, length := range cfg.RejectedLengths {
		if length < 16 || length > 8192 || rejected[length] {
			return invalidCodexTicketPolicy("拒绝长度必须在 16 到 8192 之间且不能重复")
		}
		rejected[length] = true
	}
	strict := cfg.LengthMode != config.CodexTicketLengthAuto
	if strict && rejected[cfg.TargetLength] {
		return invalidCodexTicketPolicy("兜底票据长度不能同时是拒绝长度")
	}
	seen := make(map[string]bool)
	for i := range cfg.TierRules {
		rule := &cfg.TierRules[i]
		if rule.TargetLength < 16 || rule.TargetLength > 8192 || strict && rejected[rule.TargetLength] {
			return invalidCodexTicketPolicy("套餐目标长度必须在 16 到 8192 之间且不能是拒绝长度")
		}
		if len(rule.Aliases) > 16 {
			return invalidCodexTicketPolicy("每个套餐最多 16 个别名")
		}
		if err := validateCodexTicketTierName(&rule.Tier, seen); err != nil {
			return err
		}
		for j := range rule.Aliases {
			if err := validateCodexTicketTierName(&rule.Aliases[j], seen); err != nil {
				return err
			}
		}
		if rule.Aliases == nil {
			rule.Aliases = []string{}
		}
	}
	if cfg.TierRules == nil {
		cfg.TierRules = []config.CodexTicketTierRule{}
	}
	if cfg.RejectedLengths == nil {
		cfg.RejectedLengths = []int{}
	}
	return nil
}

func validateCodexTicketTierName(name *string, seen map[string]bool) error {
	*name = strings.ToLower(strings.TrimSpace(*name))
	key := strings.NewReplacer(" ", "", "_", "", "-", "").Replace(*name)
	if !codexTicketTierNamePattern.MatchString(*name) || key == "" || seen[key] {
		return invalidCodexTicketPolicy("套餐名称和别名只能包含字母、数字、空格、下划线和连字符，归一化后不能重复")
	}
	seen[key] = true
	return nil
}

func invalidCodexTicketPolicy(message string) error {
	return infraerrors.BadRequest("INVALID_CODEX_TICKET_POLICY", message)
}
