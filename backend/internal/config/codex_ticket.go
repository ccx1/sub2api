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

	// CodexTicketRejectSafetyBuffering* 控制 Safety Buffering 否决闸的严格度。
	// off:               不因 Safety Buffering 信号否决（默认，保持既有行为）。
	// faster_model_only: 仅当服务端已声明降级模型（X-Codex-Safety-Buffering-Faster-Model 非空）时否决。
	// any:               只要 X-Codex-Safety-Buffering-Enabled=true 就否决。
	CodexTicketRejectSafetyBufferingOff         = "off"
	CodexTicketRejectSafetyBufferingFasterModel = "faster_model_only"
	CodexTicketRejectSafetyBufferingAny         = "any"

	// CodexTicketWorkspaceOriginRouting* 控制 B2：是否把账号声明的
	// workspace_backend_origin 作为打票探测的目标 host。
	// off:   始终使用默认 chatgpt.com（默认，仅 B1 采集，不改路由）。
	// probe: 使用 origin，但带健康探测；连续失败达阈值后静默回落默认 host，
	//        半开窗口再探测恢复。
	CodexTicketWorkspaceOriginRoutingOff   = "off"
	CodexTicketWorkspaceOriginRoutingProbe = "probe"

	// CodexTicketUsage* 控制业务请求的取票方式。
	// immediate: 即取即用，按票池顺序取首张可用票（默认，保持既有行为）。
	// aged:      只取首次采集后已沉淀满 MinTicketAgeSeconds 的票，最老的优先。
	CodexTicketUsageImmediate = "immediate"
	CodexTicketUsageAged      = "aged"
	// MaxCodexTicketMinAgeSeconds 是 aged 模式沉淀时长的硬上限。
	MaxCodexTicketMinAgeSeconds = 86400
)

// CodexTicketUsageAgedEnabled 报告是否启用了“只用沉淀满指定时长的票”。
func CodexTicketUsageAgedEnabled(cfg OpenAICodexTicketConfig) bool {
	return cfg.UsageMode == CodexTicketUsageAged && cfg.MinTicketAgeSeconds > 0
}

// WithoutCodexTicketUsagePolicy 去掉只影响业务取票的字段。
// 取票机制不改变票据身份：票据绑定、采集配置快照和共享调度版本都不应因它变化而失效。
func WithoutCodexTicketUsagePolicy(cfg OpenAICodexTicketConfig) OpenAICodexTicketConfig {
	cfg.UsageMode, cfg.MinTicketAgeSeconds, cfg.ConsumeAfterUse = "", 0, false
	return cfg
}

// ValidateCodexTicketUsage 校验并规范化取票机制；空值按即取即用处理，旧配置保持原行为。
func ValidateCodexTicketUsage(cfg *OpenAICodexTicketConfig) error {
	switch cfg.UsageMode {
	case "":
		cfg.UsageMode = CodexTicketUsageImmediate
	case CodexTicketUsageImmediate, CodexTicketUsageAged:
	default:
		return fmt.Errorf("取票机制必须是 immediate 或 aged")
	}
	if cfg.UsageMode != CodexTicketUsageAged {
		cfg.MinTicketAgeSeconds = 0
		return nil
	}
	limit := MaxCodexTicketMinAgeSeconds
	if cfg.TTLSeconds > 1 && cfg.TTLSeconds-1 < limit {
		limit = cfg.TTLSeconds - 1
	}
	if cfg.MinTicketAgeSeconds < 1 || cfg.MinTicketAgeSeconds > limit {
		return fmt.Errorf("min_ticket_age_seconds 必须在 1 到 %d 之间，且小于票据有效期", limit)
	}
	return nil
}

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
	switch cfg.RejectSafetyBuffering {
	case CodexTicketRejectSafetyBufferingFasterModel, CodexTicketRejectSafetyBufferingAny:
		// 保留管理员选择的严格度。
	default:
		cfg.RejectSafetyBuffering = CodexTicketRejectSafetyBufferingOff
	}
	switch cfg.WorkspaceOriginRouting {
	case CodexTicketWorkspaceOriginRoutingProbe:
		// 保留管理员选择。
	default:
		cfg.WorkspaceOriginRouting = CodexTicketWorkspaceOriginRoutingOff
	}
	if cfg.WorkspaceOriginFailureThreshold <= 0 {
		cfg.WorkspaceOriginFailureThreshold = 3
	}
	if cfg.WorkspaceOriginSilenceSeconds <= 0 {
		cfg.WorkspaceOriginSilenceSeconds = 600
	}
	// 取票机制默认即取即用；非法或缺失沉淀时长的 aged 回落即取即用，避免误配置把业务请求全部挡住。
	if cfg.UsageMode != CodexTicketUsageAged || cfg.MinTicketAgeSeconds <= 0 {
		cfg.UsageMode = CodexTicketUsageImmediate
		cfg.MinTicketAgeSeconds = 0
	}
	cfg.MinTicketAgeSeconds = min(cfg.MinTicketAgeSeconds, MaxCodexTicketMinAgeSeconds)
	return cfg
}
