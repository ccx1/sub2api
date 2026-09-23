package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketCredentialDefaults(t *testing.T) {
	cfg := NormalizeOpenAICodexTicketConfig(OpenAICodexTicketConfig{})
	require.Equal(t, CodexTicketCredentialState, cfg.CredentialMode)
	require.Equal(t, 20, cfg.CookieTTLSeconds)
	require.Equal(t, 5, *cfg.CookieRefreshBeforeSeconds)
	require.False(t, CodexTicketUsesCookies(cfg))
	require.Equal(t, 3600, cfg.TTLSeconds)
	cfg.CookieTTLSeconds, cfg.CookieRefreshBeforeSeconds = 1, nil
	cfg = NormalizeCodexTicketCredentialConfig(cfg)
	require.Zero(t, *cfg.CookieRefreshBeforeSeconds)
	zero := 0
	cfg.CookieRefreshBeforeSeconds = &zero
	require.Zero(t, *NormalizeCodexTicketCredentialConfig(cfg).CookieRefreshBeforeSeconds)
}

func TestCodexTicketCredentialValidation(t *testing.T) {
	for _, mode := range []string{CodexTicketCredentialState, CodexTicketCredentialCookieState, CodexTicketCredentialCookie} {
		cfg := OpenAICodexTicketConfig{CredentialMode: mode}
		require.NoError(t, ValidateCodexTicketCredentialConfig(&cfg))
		require.Equal(t, mode != CodexTicketCredentialState, CodexTicketUsesCookies(cfg))
	}
	negative, equal := -1, 20
	for _, cfg := range []OpenAICodexTicketConfig{
		{CredentialMode: "other"}, {CookieTTLSeconds: -1}, {CookieTTLSeconds: 3601},
		{CookieRefreshBeforeSeconds: &negative}, {CookieRefreshBeforeSeconds: &equal},
	} {
		require.Error(t, ValidateCodexTicketCredentialConfig(&cfg))
	}
}

func TestCodexTicketCredentialEnvironment(t *testing.T) {
	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_CREDENTIAL_MODE", "cookie_state")
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_COOKIE_TTL_SECONDS", "30")
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_COOKIE_REFRESH_BEFORE_SECONDS", "0")
	cfg, err := Load()
	require.NoError(t, err)
	require.Equal(t, "cookie_state", cfg.Gateway.OpenAICodexTicket.CredentialMode)
	require.Equal(t, 30, cfg.Gateway.OpenAICodexTicket.CookieTTLSeconds)
	require.NotNil(t, cfg.Gateway.OpenAICodexTicket.CookieRefreshBeforeSeconds)
	require.Zero(t, *cfg.Gateway.OpenAICodexTicket.CookieRefreshBeforeSeconds)
}

func TestCodexTicketCredentialEnvironmentRejectsInvalidPolicy(t *testing.T) {
	for _, test := range []struct{ key, value string }{
		{"CREDENTIAL_MODE", "invalid"}, {"COOKIE_TTL_SECONDS", "-1"}, {"COOKIE_REFRESH_BEFORE_SECONDS", "20"},
	} {
		t.Run(test.key, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_"+test.key, test.value)
			_, err := Load()
			require.ErrorContains(t, err, "gateway.openai_codex_ticket")
		})
	}
}
