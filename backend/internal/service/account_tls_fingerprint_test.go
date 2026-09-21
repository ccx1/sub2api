package service

import "testing"

func TestConfigureAccountProtectionOpenAIFullTLSValidation(t *testing.T) {
	account := &Account{
		ID:          1,
		Platform:    PlatformOpenAI,
		Type:        AccountTypeOAuth,
		Extra:       map[string]any{},
		Concurrency: 0,
	}

	configureAccountProtection(account)

	if err := ValidateAccountProtectionConfiguration(account); err != nil {
		t.Fatalf("ValidateAccountProtectionConfiguration() error = %v", err)
	}
	if !account.IsTLSFingerprintEnabled() {
		t.Fatalf("OpenAI OAuth account should report TLS fingerprint enabled after protection is configured")
	}
	if got := account.Extra["tls_fingerprint_builtin"]; got != "nodejs22" {
		t.Fatalf("tls_fingerprint_builtin = %v, want nodejs22", got)
	}
	if _, ok := codexFingerprintSeed(account.Extra); !ok {
		t.Fatalf("protected OpenAI account should have a persistent Codex identity seed")
	}
}

func TestIsTLSFingerprintEnabledProtocolScope(t *testing.T) {
	cases := []struct {
		name     string
		platform string
		typ      string
		want     bool
	}{
		{name: "anthropic oauth", platform: PlatformAnthropic, typ: AccountTypeOAuth, want: true},
		{name: "anthropic setup token", platform: PlatformAnthropic, typ: AccountTypeSetupToken, want: true},
		{name: "openai oauth", platform: PlatformOpenAI, typ: AccountTypeOAuth, want: true},
		{name: "openai setup token", platform: PlatformOpenAI, typ: AccountTypeSetupToken, want: true},
		{name: "openai api key", platform: PlatformOpenAI, typ: AccountTypeAPIKey, want: false},
		{name: "gemini oauth", platform: PlatformGemini, typ: AccountTypeOAuth, want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{
				Platform: tc.platform,
				Type:     tc.typ,
				Extra:    map[string]any{"enable_tls_fingerprint": true},
			}
			if got := account.IsTLSFingerprintEnabled(); got != tc.want {
				t.Fatalf("IsTLSFingerprintEnabled() = %v, want %v", got, tc.want)
			}
		})
	}
}
