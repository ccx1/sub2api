//go:build unit

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

var sharedCurrentOpenAIPlans = []string{
	"free", "go", "plus", "prolite", "pro", "promax", "team",
	"self_serve_business_usage_based", "self_serve_business_prolite", "business",
	"enterprise", "ent26", "enterprise_cbp_usage_based", "enterprise_cbp_automation",
	"edu", "edu_plus", "edu_pro",
}

func sharedTierTestToken(t *testing.T, tier string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"https://api.openai.com/auth": map[string]string{
		"chatgpt_account_id": "tier-fixture", "chatgpt_plan_type": tier,
	}})
	require.NoError(t, err)
	return "header." + base64.RawURLEncoding.EncodeToString(payload) + ".signature"
}

func TestSharedPoolCurrentOpenAIPlanSources(t *testing.T) {
	for _, tier := range sharedCurrentOpenAIPlans {
		t.Run(tier, func(t *testing.T) {
			for _, key := range []string{"plan_type", "chatgpt_plan_type", "subscription_tier", "id_token", "access_token"} {
				value := tier
				if strings.HasSuffix(key, "token") {
					value = sharedTierTestToken(t, tier)
				}
				a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{key: value}}
				require.Equal(t, tier, SharedPoolOverviewTierForAccount(a), key)
				a.Type = AccountTypeAPIKey
				require.Equal(t, "api_key", SharedPoolOverviewTierForAccount(a), key)
			}
			alias := " " + strings.ReplaceAll(strings.ToUpper(tier), "_", "-") + " "
			got, err := NormalizeSharedPoolTierOverride(PlatformOpenAI, AccountTypeOAuth, alias)
			require.NoError(t, err)
			require.Equal(t, tier, got)
		})
	}
}

func TestSharedPoolCurrentOpenAIPlanPrecedence(t *testing.T) {
	for _, tier := range sharedCurrentOpenAIPlans {
		t.Run(tier, func(t *testing.T) {
			a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
				"plan_type": tier, "id_token": sharedTierTestToken(t, "free"),
			}}
			require.Equal(t, tier, SharedPoolOverviewTierForAccount(a))
			a.Extra = map[string]any{SharedPoolSubscriptionTierKey: "plus"}
			require.Equal(t, "plus", SharedPoolOverviewTierForAccount(a))
			delete(a.Extra, SharedPoolSubscriptionTierKey)
			delete(a.Credentials, "plan_type")
			a.Credentials["id_token"] = sharedTierTestToken(t, tier)
			a.Credentials["chatgpt_account_id"] = "different-account"
			require.Equal(t, "unknown", SharedPoolOverviewTierForAccount(a))
		})
	}
}

func TestSharedPoolCurrentOpenAIPlanSettingsAndAssignment(t *testing.T) {
	for _, tier := range sharedCurrentOpenAIPlans {
		t.Run(tier, func(t *testing.T) {
			repo := &sharedTierSettingsRepo{}
			target := sharedTierGroup(11, PlatformOpenAI)
			target.IsExclusive = true
			s := &SharedPoolService{repo: repo, groups: sharedTierGroups{items: map[int64]*Group{
				10: sharedTierGroup(10, PlatformOpenAI), 11: target,
			}}}
			cfg := &SharedPoolSettings{MaxConcurrency: 5, SettlementMultiplier: 1,
				DefaultGroupIDs:                   SharedPoolDefaultGroupIDs{PlatformOpenAI: {10}},
				SubscriptionGroupIDs:              SharedPoolSubscriptionGroupIDs{PlatformOpenAI: {tier: {11}}},
				SubscriptionSettlementMultipliers: map[string]map[string]float64{PlatformOpenAI: {tier: 2}},
			}
			require.NoError(t, s.SaveSettings(context.Background(), cfg))
			a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth,
				Credentials: map[string]any{"plan_type": tier}, Extra: map[string]any{SharedPoolDispatchConsentKey: true}}
			ids, err := s.initialSharedGroups(context.Background(), repo.cfg, a)
			require.NoError(t, err)
			require.Equal(t, []int64{11}, ids)
			require.Equal(t, 2.0, EffectiveSharedSettlementMultiplierForTier(repo.cfg, nil, 7, a.Platform, SharedPoolOverviewTierForAccount(a)))
			a.Extra[SharedPoolDispatchConsentKey] = false
			ids, err = s.initialSharedGroups(context.Background(), repo.cfg, a)
			require.NoError(t, err)
			require.Equal(t, []int64{10}, ids, "未授权不能进入套餐专属组")
		})
	}
}
