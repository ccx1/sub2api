package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolOverviewTierClassification(t *testing.T) {
	for _, tc := range []struct{ platform, kind, plan, want string }{
		{PlatformOpenAI, AccountTypeOAuth, "Free", "free"},
		{PlatformOpenAI, AccountTypeOAuth, " PLUS ", "plus"},
		{PlatformOpenAI, AccountTypeOAuth, "ChatGPT_Pro", "pro"},
		{PlatformOpenAI, AccountTypeOAuth, "pro-lite", "prolite"},
		{PlatformOpenAI, AccountTypeOAuth, "Team", "team"},
		{PlatformOpenAI, AccountTypeOAuth, "self_serve_business_prolite", "self_serve_business_prolite"},
		{PlatformOpenAI, AccountTypeOAuth, "", "unknown"},
		{PlatformOpenAI, AccountTypeOAuth, "future_plan", "unknown"},
		{PlatformOpenAI, AccountTypeAPIKey, "pro", "api_key"},
		{PlatformAnthropic, AccountTypeAPIKey, "", "api_key"},
		{PlatformAnthropic, AccountTypeOAuth, "free", "unknown"},
		{PlatformAntigravity, AccountTypeOAuth, "g1-ultra-tier", "ultra"},
	} {
		t.Run(tc.platform+"/"+tc.kind+"/"+tc.plan, func(t *testing.T) {
			account := &Account{Platform: tc.platform, Type: tc.kind, Credentials: map[string]any{"plan_type": tc.plan}}
			require.Equal(t, tc.want, SharedPoolOverviewTierForAccount(account))
		})
	}
	require.Equal(t, "unknown", SharedPoolOverviewTierForAccount(nil))
	for _, tc := range []struct{ oauth, tier, want string }{
		{"", "STANDARD", "gcp_standard"}, {"", "google_ai_ultra", "google_ai_ultra"},
		{"code_assist", "google_ai_pro", "unknown"}, {"", "", "unknown"},
	} {
		account := &Account{Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"oauth_type": tc.oauth, "tier_id": tc.tier}}
		require.Equal(t, tc.want, SharedPoolOverviewTierForAccount(account))
	}
}

func TestSharedPoolOverviewDeduplicatesAndRetainsUnavailableTiers(t *testing.T) {
	accounts := []SharedPoolOverviewAccount{
		{AccountID: 1, Platform: PlatformOpenAI, Tier: "plus", Valid: true, Available: true, Concurrency: 3},
		{AccountID: 1, Platform: PlatformOpenAI, Tier: "plus", Valid: true, Available: true, Concurrency: 3},
		{AccountID: 2, Platform: PlatformOpenAI, Tier: "plus", Valid: true, Concurrency: 100},
		{AccountID: 3, Platform: PlatformOpenAI, Tier: "free"},
		{AccountID: 4, Platform: PlatformAnthropic, Tier: "api_key", Valid: true, Available: true, Concurrency: 5},
		{AccountID: 5, Platform: PlatformGemini},
	}
	got := buildSharedPoolOverview(accounts, config.OpenAICodexTicketConfig{}, sharedTicketProgressNow)
	require.EqualValues(t, 5, got.TotalAccounts)
	require.EqualValues(t, 3, got.AvailableAccounts)
	require.EqualValues(t, 2, got.ParticipatingAccounts)
	require.EqualValues(t, 2, got.SchedulableAccounts)
	require.EqualValues(t, 8, got.ConcurrencyCapacity)
	require.Nil(t, got.CurrentConcurrency)
	require.Equal(t, sharedTicketProgressNow, got.UpdatedAt)
	require.Len(t, got.Tiers, 4)
	byTier := map[string]SharedPoolOverviewTier{}
	for _, tier := range got.Tiers {
		byTier[tier.Tier] = tier
	}
	require.EqualValues(t, 2, byTier["plus"].TotalAccounts)
	require.EqualValues(t, 1, byTier["plus"].SchedulableAccounts)
	require.False(t, byTier["free"].Available)
	require.Nil(t, byTier["free"].CurrentConcurrency)
	require.False(t, byTier["unknown"].Available)
}

func TestSharedPoolOverviewTicketGatingDoesNotHideSupply(t *testing.T) {
	account := sharedTicketProgressAccount()
	account.Extra[openAICodexTicketExtraKey("gpt-5.5")] = sharedTicketProgressRaw(291, sharedTicketProgressNow.Add(time.Hour))
	accounts := []SharedPoolOverviewAccount{{AccountID: account.ID, Platform: PlatformOpenAI, Tier: "pro",
		Available: true, Valid: true, TicketRequired: true, Concurrency: 0, UntrackedConcurrency: true, Ticket: NewSharedPoolTicketAccountSnapshot(account, sharedTicketProgressNow)}}
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, TargetLength: 292, Models: []string{"gpt-5.5"}}
	got := buildSharedPoolOverview(accounts, cfg, sharedTicketProgressNow)
	require.EqualValues(t, 1, got.TotalAccounts)
	require.Zero(t, got.SchedulableAccounts)
	require.False(t, got.ConcurrencyUnlimited)
	require.False(t, got.Tiers[0].Available)
	require.Nil(t, got.CurrentConcurrency)
	require.EqualValues(t, 1, got.AvailableAccounts)
	require.Zero(t, got.ParticipatingAccounts)
	require.False(t, got.ParticipatingConcurrencyUnlimited)
	account.Extra[openAICodexTicketExtraKey("gpt-5.5")] = sharedTicketProgressRaw(292, sharedTicketProgressNow.Add(time.Hour))
	accounts[0].Ticket = NewSharedPoolTicketAccountSnapshot(account, sharedTicketProgressNow)
	got = buildSharedPoolOverview(accounts, cfg, sharedTicketProgressNow)
	require.EqualValues(t, 1, got.SchedulableAccounts)
	require.EqualValues(t, 1, got.ParticipatingAccounts)
	require.True(t, got.ParticipatingConcurrencyUnlimited)
	require.True(t, got.Tiers[0].Available)
	require.True(t, got.ConcurrencyUnlimited)
	require.Nil(t, got.CurrentConcurrency)
}

type sharedOverviewRepoStub struct {
	SharedPoolRepository
	settings    *SharedPoolSettings
	rates       []SharedPoolUserRate
	accounts    []SharedPoolOverviewAccount
	settingsErr error
	ratesErr    error
	sourceErr   error
}

func (r *sharedOverviewRepoStub) SharedSettings(context.Context) (*SharedPoolSettings, error) {
	return r.settings, r.settingsErr
}
func (r *sharedOverviewRepoStub) SharedUserRates(context.Context) ([]SharedPoolUserRate, error) {
	return r.rates, r.ratesErr
}
func (r *sharedOverviewRepoStub) SharedPoolOverviewAccounts(context.Context) ([]SharedPoolOverviewAccount, error) {
	return r.accounts, r.sourceErr
}

type sharedOverviewGroupStub struct {
	GroupRepository
	SharedPoolOverviewSource
}

func TestSharedPoolOverviewAppliesCurrentUserRatesIncludingZero(t *testing.T) {
	repo := &sharedOverviewRepoStub{settings: &SharedPoolSettings{SettlementMultiplier: 2, PlatformRateBPS: 500, ProxyRateBPS: 100},
		rates:     []SharedPoolUserRate{{UserID: 7, SettlementMultiplier: new(0.0), PlatformRateBPS: new(0)}},
		sourceErr: errors.New("fallback must not be called")}
	groupSource := &sharedOverviewRepoStub{accounts: []SharedPoolOverviewAccount{{AccountID: 5, Platform: PlatformOpenAI, Tier: "free"}}}
	svc := &SharedPoolService{repo: repo, groups: &sharedOverviewGroupStub{SharedPoolOverviewSource: groupSource}}
	got, err := svc.Overview(context.Background(), 7, config.OpenAICodexTicketConfig{})
	require.NoError(t, err)
	require.Zero(t, got.SettlementMultiplier)
	require.Zero(t, got.PlatformRateBPS)
	require.Equal(t, 100, got.ProxyRateBPS)
	require.EqualValues(t, 1, got.TotalAccounts)
	got, err = svc.Overview(context.Background(), 8, config.OpenAICodexTicketConfig{})
	require.NoError(t, err)
	require.Equal(t, 2.0, got.SettlementMultiplier)
	require.Equal(t, 500, got.PlatformRateBPS)
}

func TestSharedPoolOverviewFailsOnSourceAndConfigurationErrors(t *testing.T) {
	failure := errors.New("read unavailable")
	for _, repo := range []*sharedOverviewRepoStub{
		{settingsErr: failure}, {settings: &SharedPoolSettings{}, ratesErr: failure},
		{settings: &SharedPoolSettings{}, sourceErr: failure},
	} {
		svc := &SharedPoolService{repo: repo}
		got, err := svc.Overview(context.Background(), 7, config.OpenAICodexTicketConfig{})
		require.ErrorIs(t, err, failure)
		require.Nil(t, got)
	}
	got, err := (&SharedPoolService{}).Overview(context.Background(), 7, config.OpenAICodexTicketConfig{})
	require.Error(t, err)
	require.Nil(t, got)
}

func TestSharedPoolOverviewConcurrencyBatchesOnceWithoutDuplicatedTotals(t *testing.T) {
	accounts := []SharedPoolOverviewAccount{
		{AccountID: 1, Platform: PlatformOpenAI, Tier: "plus", Available: true, Concurrency: 3},
		{AccountID: 2, Platform: PlatformAnthropic, Tier: "api_key", Available: true, Concurrency: 5},
		{AccountID: 3, Platform: PlatformOpenAI, Tier: "free", Concurrency: 2},
	}
	cache := &sharedPoolConcurrencyCache{counts: map[int64]int{1: 2, 2: 4, 3: 1}}
	svc := &APIKeyService{concurrencyService: NewConcurrencyService(cache)}
	got := buildSharedPoolOverview(accounts, config.OpenAICodexTicketConfig{}, sharedTicketProgressNow)
	svc.EnrichSharedPoolOverviewConcurrency(context.Background(), got)
	require.EqualValues(t, 7, *got.CurrentConcurrency)
	require.Len(t, cache.queries, 1)
	require.ElementsMatch(t, []int64{1, 2, 3}, cache.queries[0])
	for _, tier := range got.Tiers {
		want := map[string]int64{"api_key": 4, "plus": 2, "free": 1}[tier.Tier]
		require.Equal(t, want, *tier.CurrentConcurrency)
	}
	cache.err = errors.New("redis unavailable")
	svc.EnrichSharedPoolOverviewConcurrency(context.Background(), got)
	require.Nil(t, got.CurrentConcurrency)
	for _, tier := range got.Tiers {
		require.Nil(t, tier.CurrentConcurrency)
	}
}

func TestSharedPoolOverviewConcurrencyIncludesOutstandingRequestsWithoutTicket(t *testing.T) {
	account := sharedTicketProgressAccount()
	account.Extra[openAICodexTicketExtraKey("gpt-5.5")] = sharedTicketProgressRaw(291, sharedTicketProgressNow.Add(time.Hour))
	accounts := []SharedPoolOverviewAccount{{AccountID: account.ID, Platform: PlatformOpenAI, Tier: "pro", Available: true,
		Concurrency: 3, Ticket: NewSharedPoolTicketAccountSnapshot(account, sharedTicketProgressNow)}}
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, TargetLength: 292, Models: []string{"gpt-5.5"}}
	overview := buildSharedPoolOverview(accounts, cfg, sharedTicketProgressNow)
	cache := &sharedPoolConcurrencyCache{counts: map[int64]int{account.ID: 1}}
	api := &APIKeyService{concurrencyService: NewConcurrencyService(cache)}
	api.EnrichSharedPoolOverviewConcurrency(context.Background(), overview, cfg)
	require.Equal(t, int64(1), *overview.CurrentConcurrency)
	require.Equal(t, [][]int64{{account.ID}}, cache.queries, "未命中 292 门票也可能仍有在途请求")
}

func TestSharedPoolOverviewSerializationContainsOnlyPublicFields(t *testing.T) {
	account := sharedTicketProgressAccount()
	account.Extra[openAICodexTicketExtraKey("gpt-5.5")] = sharedTicketProgressRaw(292, sharedTicketProgressNow.Add(time.Hour))
	input := SharedPoolOverviewAccount{AccountID: 987654321, Platform: PlatformOpenAI, Tier: "pro", Available: true,
		Concurrency: 3, Ticket: NewSharedPoolTicketAccountSnapshot(account, sharedTicketProgressNow)}
	got := buildSharedPoolOverview([]SharedPoolOverviewAccount{input}, config.OpenAICodexTicketConfig{}, sharedTicketProgressNow)
	raw, err := json.Marshal(got)
	require.NoError(t, err)
	var public map[string]any
	require.NoError(t, json.Unmarshal(raw, &public))
	require.Len(t, public, 14)
	for _, key := range []string{"total_accounts", "available_accounts", "schedulable_accounts", "concurrency_capacity", "concurrency_unlimited",
		"current_concurrency", "settlement_multiplier", "platform_rate_bps", "proxy_rate_bps", "updated_at", "tiers",
		"participating_accounts", "participating_concurrency", "participating_concurrency_unlimited"} {
		require.Contains(t, public, key)
	}
	tier := public["tiers"].([]any)[0].(map[string]any)
	require.Len(t, tier, 12)
	for _, forbidden := range []string{"987654321", "credential-sentinel", "account_id", "owner", "email", "group", "tickets", "state"} {
		require.NotContains(t, string(raw), forbidden)
	}
	internal, err := json.Marshal(input)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(internal))
}
