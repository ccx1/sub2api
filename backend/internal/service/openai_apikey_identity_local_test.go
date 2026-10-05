//go:build unit

package service

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestApplyOpenAIAPIKeyIdentityHeaders(t *testing.T) {
	SetCodexCanonicalUserAgentResolver(func() string {
		return openai.CodexDefaultOriginator + "/0.200.1" + codexCLIUserAgentSuffix
	})
	t.Cleanup(func() {
		SetCodexCanonicalUserAgentResolver(nil)
		SetCodexIdentityEnforcementEnabled(true)
	})
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	headers := http.Header{
		"User-Agent": {"Mozilla/5.0"},
		"Originator": {"untrusted"},
		"Version":    {"0.1.0"},
	}
	applyOpenAIAPIKeyIdentityHeaders(headers, account, "")
	require.Equal(t, openai.CodexDefaultOriginator+"/0.200.1"+codexCLIUserAgentSuffix, headers.Get("User-Agent"))
	require.Equal(t, openai.CodexDefaultOriginator, headers.Get("Originator"))
	require.Equal(t, "0.200.1", headers.Get("Version"))
}

func TestApplyOpenAIAPIKeyIdentityHeadersKeepsOptOutAndNonOpenAI(t *testing.T) {
	for _, tc := range []struct {
		name    string
		account *Account
		enabled bool
	}{
		{name: "disabled", account: &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, enabled: false},
		{name: "other_platform", account: &Account{Platform: PlatformDeepseek, Type: AccountTypeAPIKey}, enabled: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			SetCodexIdentityEnforcementEnabled(tc.enabled)
			headers := http.Header{"User-Agent": {"client/1"}, "Originator": {"client"}, "Version": {"1.0"}}
			applyOpenAIAPIKeyIdentityHeaders(headers, tc.account, "")
			require.Equal(t, "client/1", headers.Get("User-Agent"))
			require.Equal(t, "client", headers.Get("Originator"))
			require.Equal(t, "1.0", headers.Get("Version"))
		})
	}
}
