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
	require.EqualValues(t, 1, got.ParticipatingAccounts, "票据总开关关闭后仅 API Key 账号计入参与")
	require.EqualValues(t, 4, got.AvailableAccounts)
	require.EqualValues(t, 5, got.TotalAccounts)
}
