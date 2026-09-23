package config

import "fmt"

const (
	CodexTicketCredentialState                   = "state"
	CodexTicketCredentialCookieState             = "cookie_state"
	CodexTicketCredentialCookie                  = "cookie"
	DefaultCodexTicketCookieTTLSeconds           = 20
	DefaultCodexTicketCookieRefreshBeforeSeconds = 5
	MaxCodexTicketCookieTTLSeconds               = 3600
)

func CodexTicketUsesCookies(cfg OpenAICodexTicketConfig) bool {
	return cfg.CredentialMode == CodexTicketCredentialCookieState || cfg.CredentialMode == CodexTicketCredentialCookie
}

func NormalizeCodexTicketCredentialConfig(cfg OpenAICodexTicketConfig) OpenAICodexTicketConfig {
	if cfg.CredentialMode == "" {
		cfg.CredentialMode = CodexTicketCredentialState
	}
	if cfg.CookieTTLSeconds == 0 {
		cfg.CookieTTLSeconds = DefaultCodexTicketCookieTTLSeconds
	}
	refresh := min(DefaultCodexTicketCookieRefreshBeforeSeconds, max(0, cfg.CookieTTLSeconds-1))
	if cfg.CookieRefreshBeforeSeconds != nil {
		refresh = *cfg.CookieRefreshBeforeSeconds
	}
	cfg.CookieRefreshBeforeSeconds = &refresh
	return cfg
}

func ValidateCodexTicketCredentialConfig(cfg *OpenAICodexTicketConfig) error {
	*cfg = NormalizeCodexTicketCredentialConfig(*cfg)
	switch cfg.CredentialMode {
	case CodexTicketCredentialState, CodexTicketCredentialCookieState, CodexTicketCredentialCookie:
	default:
		return fmt.Errorf("凭据模式必须是 state、cookie_state 或 cookie")
	}
	if cfg.CookieTTLSeconds < 1 || cfg.CookieTTLSeconds > MaxCodexTicketCookieTTLSeconds {
		return fmt.Errorf("Cookie 有效期必须在 1 到 %d 秒之间", MaxCodexTicketCookieTTLSeconds)
	}
	if refresh := *cfg.CookieRefreshBeforeSeconds; refresh < 0 || refresh >= cfg.CookieTTLSeconds {
		return fmt.Errorf("Cookie 提前刷新时间必须大于等于 0 且小于有效期")
	}
	return nil
}
