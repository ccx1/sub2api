package config

// CodexTicketProtectionConfig 包含采集保护和出口 IP 保护；后者同时限制打票与业务代理使用。
type CodexTicketProtectionConfig struct {
	Enabled                       bool  `mapstructure:"enabled" json:"enabled"`
	RejectAndSilenceLengths       []int `mapstructure:"reject_and_silence_lengths" json:"reject_and_silence_lengths"`
	ProxySilenceSeconds           int   `mapstructure:"proxy_silence_seconds" json:"proxy_silence_seconds"`
	MaxPoolRounds                 int   `mapstructure:"max_pool_rounds" json:"max_pool_rounds"`
	MaxAccountAttempts            int   `mapstructure:"max_account_attempts" json:"max_account_attempts"`
	AccountCooldownSeconds        int   `mapstructure:"account_cooldown_seconds" json:"account_cooldown_seconds"`
	RejectionRetryIntervalSeconds int   `mapstructure:"rejection_retry_interval_seconds" json:"rejection_retry_interval_seconds"`
	RejectionRetryMaxAttempts     int   `mapstructure:"rejection_retry_max_attempts" json:"rejection_retry_max_attempts"`
	RejectionRetryCooldownSeconds int   `mapstructure:"rejection_retry_cooldown_seconds" json:"rejection_retry_cooldown_seconds"`
	// IP 保护独立启用，旧配置不能因升级而自动禁用出口。
	ProxyIPProtectionEnabled       bool `mapstructure:"proxy_ip_protection_enabled" json:"proxy_ip_protection_enabled"`
	ProxyIPFailureAccountThreshold int  `mapstructure:"proxy_ip_failure_account_threshold" json:"proxy_ip_failure_account_threshold"`
	ProxyIPFailureWindowSeconds    int  `mapstructure:"proxy_ip_failure_window_seconds" json:"proxy_ip_failure_window_seconds"`
	ProxyIPCooldownSeconds         int  `mapstructure:"proxy_ip_cooldown_seconds" json:"proxy_ip_cooldown_seconds"`
	ProxyIPMaxRounds               int  `mapstructure:"proxy_ip_max_rounds" json:"proxy_ip_max_rounds"`
}

func DefaultCodexTicketProtection() CodexTicketProtectionConfig {
	return CodexTicketProtectionConfig{
		RejectAndSilenceLengths: []int{312}, ProxySilenceSeconds: 300,
		MaxPoolRounds: 2, MaxAccountAttempts: 6, AccountCooldownSeconds: 1800,
		RejectionRetryIntervalSeconds: 30, RejectionRetryMaxAttempts: 6, RejectionRetryCooldownSeconds: 300,
		ProxyIPFailureAccountThreshold: 3, ProxyIPFailureWindowSeconds: 600, ProxyIPCooldownSeconds: 300, ProxyIPMaxRounds: 3,
	}
}

func (cfg OpenAICodexTicketConfig) TicketProtection() CodexTicketProtectionConfig {
	if cfg.Protection == nil {
		return DefaultCodexTicketProtection()
	}
	value := *cfg.Protection
	defaults := DefaultCodexTicketProtection()
	if value.RejectionRetryIntervalSeconds == 0 {
		value.RejectionRetryIntervalSeconds = defaults.RejectionRetryIntervalSeconds
	}
	if value.RejectionRetryMaxAttempts == 0 {
		value.RejectionRetryMaxAttempts = defaults.RejectionRetryMaxAttempts
	}
	if value.RejectionRetryCooldownSeconds == 0 {
		value.RejectionRetryCooldownSeconds = defaults.RejectionRetryCooldownSeconds
	}
	if value.ProxyIPFailureAccountThreshold == 0 {
		value.ProxyIPFailureAccountThreshold = defaults.ProxyIPFailureAccountThreshold
	}
	if value.ProxyIPFailureWindowSeconds == 0 {
		value.ProxyIPFailureWindowSeconds = defaults.ProxyIPFailureWindowSeconds
	}
	if value.ProxyIPCooldownSeconds == 0 {
		value.ProxyIPCooldownSeconds = defaults.ProxyIPCooldownSeconds
	}
	if value.ProxyIPMaxRounds == 0 {
		value.ProxyIPMaxRounds = defaults.ProxyIPMaxRounds
	}
	value.RejectAndSilenceLengths = append([]int{}, value.RejectAndSilenceLengths...)
	return value
}
