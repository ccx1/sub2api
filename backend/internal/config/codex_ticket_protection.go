package config

// CodexTicketProtectionConfig 只约束采集调度，不参与已有业务票据绑定。
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
}

func DefaultCodexTicketProtection() CodexTicketProtectionConfig {
	return CodexTicketProtectionConfig{
		RejectAndSilenceLengths: []int{312}, ProxySilenceSeconds: 300,
		MaxPoolRounds: 2, MaxAccountAttempts: 6, AccountCooldownSeconds: 1800,
		RejectionRetryIntervalSeconds: 30, RejectionRetryMaxAttempts: 6, RejectionRetryCooldownSeconds: 300,
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
	value.RejectAndSilenceLengths = append([]int{}, value.RejectAndSilenceLengths...)
	return value
}
