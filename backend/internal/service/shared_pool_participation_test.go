package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolOverviewSeparatesTicketsValidityAndSupply(t *testing.T) {
	account := sharedTicketProgressAccount()
	account.Extra[openAICodexTicketExtraKey("gpt-5.5")] = sharedTicketProgressRaw(292, sharedTicketProgressNow.Add(time.Hour))
	ticket := NewSharedPoolTicketAccountSnapshot(account, sharedTicketProgressNow)
	accounts := []SharedPoolOverviewAccount{
		{AccountID: 1, Platform: PlatformOpenAI, Tier: "pro", Valid: true, Available: true, TicketRequired: true, Ticket: ticket, Concurrency: 3},
		{AccountID: 1, Platform: PlatformOpenAI, Tier: "pro", Valid: true, Available: true, TicketRequired: true, Ticket: ticket, Concurrency: 3},
		{AccountID: 2, Platform: PlatformOpenAI, Tier: "pro", Valid: true, Available: true, TicketRequired: true, Concurrency: 5},
		{AccountID: 3, Platform: PlatformOpenAI, Tier: "pro", Valid: true, TicketRequired: true, Ticket: ticket, Concurrency: 10},
		{AccountID: 4, Platform: PlatformOpenAI, Tier: "pro", Available: true, TicketRequired: true, Ticket: ticket, Concurrency: 20},
		{AccountID: 5, Platform: PlatformOpenAI, Tier: "api_key", Valid: true, Available: true, Concurrency: 2},
	}
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: false, TargetLength: 292, Models: []string{"gpt-5.5"}}
	got := buildSharedPoolOverview(accounts, cfg, sharedTicketProgressNow)
	require.EqualValues(t, 5, got.TotalAccounts)
	require.EqualValues(t, 4, got.AvailableAccounts)
	require.EqualValues(t, 2, got.ParticipatingAccounts)
	require.EqualValues(t, 5, got.ParticipatingConcurrency)
	require.False(t, got.ParticipatingConcurrencyUnlimited)
	for _, tier := range got.Tiers {
		require.EqualValues(t, 1, tier.ParticipatingAccounts)
		require.True(t, tier.Available)
		if tier.Tier == "pro" {
			require.EqualValues(t, 3, tier.AvailableAccounts)
			require.EqualValues(t, 4, tier.TotalAccounts)
		}
	}
	cache := &sharedPoolConcurrencyCache{counts: map[int64]int{1: 1, 2: 2, 3: 4, 4: 0, 5: 0}}
	svc := &APIKeyService{concurrencyService: NewConcurrencyService(cache)}
	svc.EnrichSharedPoolOverviewConcurrency(context.Background(), got)
	require.NotNil(t, got.CurrentConcurrency)
	require.EqualValues(t, 7, *got.CurrentConcurrency, "停用账号的在途请求也计入当前占用")
	require.Len(t, cache.queries, 1)
	require.ElementsMatch(t, []int64{1, 2, 3, 4, 5}, cache.queries[0])
	cfg.Enabled = false
	got = buildSharedPoolOverview(accounts, cfg, sharedTicketProgressNow)
	require.EqualValues(t, 3, got.ParticipatingAccounts, "关闭打票后，有效且允许调度的账号无需票据即可参与")
	require.EqualValues(t, 10, got.ParticipatingConcurrency)
	require.EqualValues(t, 4, got.AvailableAccounts)
	require.EqualValues(t, 5, got.TotalAccounts)
}

func TestSharedPoolOverviewParticipationHonorsTicketSwitches(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		global, enabled, ready          bool
		valid, available, participating bool
	}{
		{"account tickets disabled", true, false, false, true, true, true},
		{"global tickets disabled", false, true, false, true, true, true},
		{"both switches disabled", false, false, false, true, true, true},
		{"enabled without ticket", true, true, false, true, true, false},
		{"enabled with ready ticket", true, true, true, true, true, true},
		{"account disabled does not bypass dispatch", true, false, false, true, false, false},
		{"global disabled does not bypass dispatch", false, true, false, true, false, false},
		{"account disabled does not bypass validity", true, false, false, false, true, false},
		{"global disabled does not bypass validity", false, true, false, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := sharedTicketProgressAccount()
			account.Extra[OpenAICodexTicketEnabledExtraKey] = tc.enabled
			if tc.ready {
				account.Extra[openAICodexTicketExtraKey("gpt-5.5")] = sharedTicketProgressRaw(292, sharedTicketProgressNow.Add(time.Hour))
			}
			accounts := []SharedPoolOverviewAccount{{AccountID: account.ID, Platform: PlatformOpenAI, Tier: "plus",
				Valid: tc.valid, Available: tc.available, Concurrency: 3, TicketRequired: OpenAICodexTicketAccountEnabled(account),
				Ticket: NewSharedPoolTicketAccountSnapshot(account, sharedTicketProgressNow)}}
			cfg := config.OpenAICodexTicketConfig{Enabled: tc.global, FailClosed: true, TargetLength: 292, Models: []string{"gpt-5.5"}}
			got := buildSharedPoolOverview(accounts, cfg, sharedTicketProgressNow)
			want := int64(0)
			if tc.participating {
				want = 1
			}
			require.EqualValues(t, 1, got.TotalAccounts)
			require.Equal(t, want, got.ParticipatingAccounts)
			require.Equal(t, want*3, got.ParticipatingConcurrency)
			require.Len(t, got.Tiers, 1)
			require.Equal(t, want, got.Tiers[0].ParticipatingAccounts)
			require.Equal(t, want*3, got.Tiers[0].ParticipatingConcurrency)
			require.Equal(t, tc.participating, got.Tiers[0].Available)
		})
	}
}
