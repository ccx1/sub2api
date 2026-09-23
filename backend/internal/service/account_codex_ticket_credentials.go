package service

import (
	"bytes"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const CodexTicketCredentialPolicyExtraKey = "codex_ticket_credential_policy"

type codexTicketCredentialPolicy struct {
	Mode                 string `json:"mode"`
	TTLSeconds           *int   `json:"ttl_seconds,omitempty"`
	RefreshBeforeSeconds *int   `json:"refresh_before_seconds,omitempty"`
}

func parseCodexTicketCredentialPolicy(raw any) (codexTicketCredentialPolicy, error) {
	policy := codexTicketCredentialPolicy{}
	if raw == nil {
		policy.Mode = "inherit"
		return policy, nil
	}
	payload, err := json.Marshal(raw)
	if err != nil {
		return policy, invalidCodexTicketCredentialPolicy("打票凭据配置格式无效")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return policy, invalidCodexTicketCredentialPolicy("打票凭据配置字段或数值格式无效")
	}
	switch policy.Mode {
	case "inherit":
		policy.TTLSeconds, policy.RefreshBeforeSeconds = nil, nil
	case config.CodexTicketCredentialState, config.CodexTicketCredentialCookieState, config.CodexTicketCredentialCookie:
	default:
		return policy, invalidCodexTicketCredentialPolicy("打票凭据模式必须是 inherit、state、cookie_state 或 cookie")
	}
	if policy.TTLSeconds != nil && (*policy.TTLSeconds < 1 || *policy.TTLSeconds > config.MaxCodexTicketCookieTTLSeconds) {
		return policy, invalidCodexTicketCredentialPolicy("Cookie 有效期必须在 1 到 3600 秒之间")
	}
	if refresh := policy.RefreshBeforeSeconds; refresh != nil && (*refresh < 0 || *refresh >= config.MaxCodexTicketCookieTTLSeconds || policy.TTLSeconds != nil && *refresh >= *policy.TTLSeconds) {
		return policy, invalidCodexTicketCredentialPolicy("Cookie 提前刷新时间必须大于等于 0 且小于有效期")
	}
	return policy, nil
}

func normalizeCodexTicketCredentialExtra(extra map[string]any) error {
	raw, supplied := extra[CodexTicketCredentialPolicyExtraKey]
	if !supplied || raw == nil {
		return nil
	}
	policy, err := parseCodexTicketCredentialPolicy(raw)
	if err != nil {
		return err
	}
	value := map[string]any{"mode": policy.Mode}
	if policy.TTLSeconds != nil {
		value["ttl_seconds"] = *policy.TTLSeconds
	}
	if policy.RefreshBeforeSeconds != nil {
		value["refresh_before_seconds"] = *policy.RefreshBeforeSeconds
	}
	extra[CodexTicketCredentialPolicyExtraKey] = value
	return nil
}

func resolveCodexTicketCredentialConfig(account *Account, cfg config.OpenAICodexTicketConfig) config.OpenAICodexTicketConfig {
	cfg = config.NormalizeCodexTicketCredentialConfig(cfg)
	if account != nil {
		policy, err := parseCodexTicketCredentialPolicy(account.Extra[CodexTicketCredentialPolicyExtraKey])
		if err == nil && policy.Mode != "inherit" {
			cfg.CredentialMode = policy.Mode
			if policy.TTLSeconds != nil {
				cfg.CookieTTLSeconds = *policy.TTLSeconds
			}
			if policy.RefreshBeforeSeconds != nil {
				refresh := *policy.RefreshBeforeSeconds
				cfg.CookieRefreshBeforeSeconds = &refresh
			}
		}
	}
	if config.CodexTicketUsesCookies(cfg) {
		// 全局窗口后续变长时，账号较短的有效期仍优先，确保刷新边界有效。
		refresh := min(*cfg.CookieRefreshBeforeSeconds, cfg.CookieTTLSeconds-1)
		cfg.CookieRefreshBeforeSeconds = &refresh
		// CookieTTL is an independent credential deadline. Keep STATE TTL intact;
		// only the scheduler's refresh window follows the Cookie setting.
		cfg.RefreshBeforeSeconds = refresh
	}
	return cfg
}

func invalidCodexTicketCredentialPolicy(message string) error {
	return infraerrors.BadRequest("INVALID_CODEX_TICKET_CREDENTIAL_POLICY", message)
}
