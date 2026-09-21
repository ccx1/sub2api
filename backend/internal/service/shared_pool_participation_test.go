package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolOverviewParticipationIsIndependentOfAvailability(t *testing.T) {
	accounts := []SharedPoolOverviewAccount{
		{AccountID: 1, Platform: PlatformOpenAI, Tier: "free", Participating: true, Concurrency: 3},
		{AccountID: 1, Platform: PlatformOpenAI, Tier: "free", Participating: true, Concurrency: 3},
		{AccountID: 2, Platform: PlatformOpenAI, Tier: "pro", Participating: true, Concurrency: 5},
		{AccountID: 3, Platform: PlatformOpenAI, Tier: "pro", Concurrency: 10},
	}
	got := buildSharedPoolOverview(accounts, config.OpenAICodexTicketConfig{}, sharedTicketProgressNow)
	require.EqualValues(t, 3, got.TotalAccounts)
	require.EqualValues(t, 2, got.ParticipatingAccounts)
	require.EqualValues(t, 8, got.ParticipatingConcurrency)
	require.Zero(t, got.SchedulableAccounts)
	require.Zero(t, got.ConcurrencyCapacity)
	for _, tier := range got.Tiers {
		require.EqualValues(t, 1, tier.ParticipatingAccounts)
		require.False(t, tier.Available)
	}
	cache := &sharedPoolConcurrencyCache{counts: map[int64]int{1: 1, 2: 2, 3: 4}}
	svc := &APIKeyService{concurrencyService: NewConcurrencyService(cache)}
	svc.EnrichSharedPoolOverviewConcurrency(context.Background(), got)
	require.EqualValues(t, 7, *got.CurrentConcurrency, "停用账号的在途请求也计入当前占用")
	require.Len(t, cache.queries, 1)
	require.ElementsMatch(t, []int64{1, 2, 3}, cache.queries[0])
}
