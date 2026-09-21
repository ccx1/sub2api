package service

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedPoolSubscriptionTierUsesExplicitImportedPlan(t *testing.T) {
	token := func(plan string) string {
		return "header." + base64.RawURLEncoding.EncodeToString([]byte(`{"aud":"test","https://api.openai.com/auth":{"chatgpt_account_id":"account-1","chatgpt_plan_type":"`+plan+`"}}`)) + ".signature"
	}
	for _, tc := range []struct {
		name        string
		credentials map[string]any
		want        string
	}{
		{"free id token", map[string]any{"id_token": token("free")}, "free"},
		{"free access token", map[string]any{"access_token": token("free")}, "free"},
		{"free alias", map[string]any{"plan_type": "chatgpt_free"}, "free"},
		{"explicit plan alias", map[string]any{"chatgpt_plan_type": "free"}, "free"},
		{"plan preferred", map[string]any{"plan_type": "plus", "id_token": token("free")}, "plus"},
		{"unknown plan token fallback", map[string]any{"plan_type": "unknown", "id_token": token("free")}, "free"},
		{"wrong account", map[string]any{"chatgpt_account_id": "other", "id_token": token("free")}, "unknown"},
		{"missing is unknown", map[string]any{}, "unknown"},
		{"malformed is unknown", map[string]any{"id_token": "not-a-token"}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: tc.credentials}
			require.Equal(t, tc.want, SharedPoolOverviewTierForAccount(a))
		})
	}
}

func TestSharedPoolSubscriptionTierOverrideWinsAndCanReset(t *testing.T) {
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"plan_type": "free"}, Extra: map[string]any{SharedPoolSubscriptionTierKey: "pro"}}
	require.Equal(t, "pro", SharedPoolOverviewTierForAccount(a))
	delete(a.Extra, SharedPoolSubscriptionTierKey)
	require.Equal(t, "free", SharedPoolOverviewTierForAccount(a))
	tier, err := NormalizeSharedPoolTierOverride(PlatformOpenAI, AccountTypeOAuth, "Pro 20x")
	require.NoError(t, err)
	require.Equal(t, "pro", tier)
	_, err = NormalizeSharedPoolTierOverride(PlatformOpenAI, AccountTypeAPIKey, "pro")
	require.Error(t, err)
	_, err = NormalizeSharedPoolTierOverride(PlatformOpenAI, AccountTypeOAuth, "ultra")
	require.Error(t, err)
	tier, err = NormalizeSharedPoolTierOverride(PlatformOpenAI, AccountTypeOAuth, " ")
	require.NoError(t, err)
	require.Empty(t, tier)
}
