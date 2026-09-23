package config

import "fmt"

const (
	CodexTicketLengthStrict        = "strict"
	CodexTicketLengthAuto          = "auto"
	CodexTicketSessionRandom       = "random"
	CodexTicketSessionAccount      = "account"
	CodexTicketSessionAccountModel = "account_model"
	CodexTicketRefreshRevalidate   = "revalidate"
	CodexTicketRefreshReplace      = "replace"
	DefaultCodexTicketPoolCapacity = 5
	MaxCodexTicketPoolCapacity     = 20
)

// CodexTicketBusinessVerificationEnabled 让未配置此开关的旧安装继续复核业务出口。
func CodexTicketBusinessVerificationEnabled(cfg OpenAICodexTicketConfig) bool {
	return cfg.VerifyBusiness == nil || *cfg.VerifyBusiness
}

func ValidateCodexTicketRefreshStrategy(cfg *OpenAICodexTicketConfig) error {
	if cfg.RefreshStrategy == "" {
		cfg.RefreshStrategy = CodexTicketRefreshRevalidate
	}
	if cfg.RefreshStrategy != CodexTicketRefreshRevalidate && cfg.RefreshStrategy != CodexTicketRefreshReplace {
		return fmt.Errorf("自动更新策略必须是 revalidate 或 replace")
	}
	return nil
}

// NormalizeOpenAICodexTicketConfig 为旧 YAML 和未配置的安装补齐兼容默认值。
// 持久化设置先校验再读取，允许零次上限（持续按退避重试）和空拒绝长度列表。
func NormalizeOpenAICodexTicketConfig(cfg OpenAICodexTicketConfig) OpenAICodexTicketConfig {
	cfg = NormalizeCodexTicketCredentialConfig(cfg)
	if cfg.RefreshStrategy == "" {
		cfg.RefreshStrategy = CodexTicketRefreshRevalidate
	}
	if cfg.PoolCapacity <= 0 {
		cfg.PoolCapacity = DefaultCodexTicketPoolCapacity
	}
	cfg.PoolCapacity = min(cfg.PoolCapacity, MaxCodexTicketPoolCapacity)
	if cfg.VerifyBusiness == nil {
		enabled := true
		cfg.VerifyBusiness = &enabled
	}
	if cfg.BusinessVerificationRounds <= 0 {
		cfg.BusinessVerificationRounds = 1
	} else if cfg.BusinessVerificationRounds > 10 {
		cfg.BusinessVerificationRounds = 10
	}
	if cfg.SessionMode == "" {
		cfg.SessionMode = CodexTicketSessionRandom
	}
	if cfg.ProxyFailureThreshold == 0 {
		cfg.ProxyFailureThreshold = 3
	}
	// 旧配置继续使用原筛选规则；动态模式须由管理员明确选择。
	if cfg.LengthMode == "" {
		cfg.LengthMode = CodexTicketLengthStrict
	}
	if cfg.TargetLength <= 0 {
		cfg.TargetLength = 292
	}
	if cfg.TTLSeconds <= 0 {
		cfg.TTLSeconds = 3600
	}
	if cfg.RefreshBeforeSeconds <= 0 && cfg.RetryBackoffSeconds == nil {
		cfg.RefreshBeforeSeconds = 600
	}
	if cfg.HarvestProbeIntervalSeconds <= 0 {
		cfg.HarvestProbeIntervalSeconds = 6
	}
	if cfg.HarvestAttemptTimeoutSeconds <= 0 {
		cfg.HarvestAttemptTimeoutSeconds = 25
	}
	if len(cfg.Models) == 0 {
		cfg.Models = []string{"gpt-6-astra", "gpt-5.6-sol"}
	}
	if cfg.TierRules == nil {
		cfg.TierRules = []CodexTicketTierRule{
			{Tier: "team", Aliases: []string{"business", "chatgptteam", "chatgptbusiness", "business_standard"}, TargetLength: 332},
			{Tier: "pro", Aliases: []string{"chatgptpro"}, TargetLength: 292},
		}
	}
	if cfg.RejectedLengths == nil {
		cfg.RejectedLengths = []int{312}
	}
	if cfg.HarvestConcurrency <= 0 {
		cfg.HarvestConcurrency = 8
	}
	if cfg.RetryBackoffSeconds == nil {
		cfg.RetryBackoffSeconds = []int{30, 60, 300, 600, 1200, 1800}
		cfg.RetryMaxAttempts = 6
		cfg.RespectRetryAfter = true
	}
	if cfg.RetryExhaustedCooldownSeconds <= 0 {
		cfg.RetryExhaustedCooldownSeconds = 1800
	}
	if cfg.AuthCooldownSeconds <= 0 {
		cfg.AuthCooldownSeconds = 300
	}
	if cfg.RateLimitCooldownSeconds <= 0 {
		cfg.RateLimitCooldownSeconds = 300
	}
	return cfg
}
