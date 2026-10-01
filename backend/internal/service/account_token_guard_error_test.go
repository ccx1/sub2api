package service

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAccountTokenGuardReloginProtocolErrorKeepsKnownCodes(t *testing.T) {
	for _, code := range []string{
		"auth_failed", "invalid_token", "token_invalid", "requires_relogin",
		"invalid_grant", "revoked", "relogin_rejected",
	} {
		t.Run(code, func(t *testing.T) {
			for _, value := range []string{code, " \t" + strings.ToUpper(code) + "\n"} {
				err := newGuardReloginProtocolError(value)
				if err == nil || err.Error() != code {
					t.Fatalf("fixed protocol code was not retained: %v", err)
				}
			}
		})
	}
}

func TestAccountTokenGuardReloginProtocolErrorRedactsUnknownCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		code any
	}{
		{"missing", nil},
		{"empty", " "},
		{"unknown", "provider_private_failure"},
		{"token", "invalid_token: sk-test-only-canary"},
		{"password", "password=test-only-password-canary"},
		{"mfa", "mfa_secret=TESTONLYMFACANARY"},
		{"url", "https://user:test-only-password@example.invalid/?access_token=test-only-token"},
		{"json string", `{"access_token":"test-only-token","password":"test-only-password","mfa_secret":"test-only-mfa"}`},
		{"json object", map[string]any{"code": "invalid_grant", "password": "test-only-password"}},
		{"json array", []any{"invalid_token", "test-only-token"}},
		{"number", 123456},
		{"boolean", true},
		{"newline suffix", "invalid_grant\naccess_token=test-only-token"},
		{"oversized", strings.Repeat("test-only-secret", 1024)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := newGuardReloginProtocolError(tc.code)
			if err == nil || err.Error() != "relogin_rejected" {
				t.Fatal("unknown upstream content reached the diagnostic error")
			}
			raw, marshalErr := json.Marshal(map[string]string{"error": err.Error()})
			if marshalErr != nil || string(raw) != `{"error":"relogin_rejected"}` {
				t.Fatal("unknown upstream content reached the JSON response")
			}
		})
	}
}
