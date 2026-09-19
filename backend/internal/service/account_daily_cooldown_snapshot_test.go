//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type dailyCooldownSnapshotCache struct {
	SchedulerCache
	snapshot []*Account
	hit      bool
}

func (c *dailyCooldownSnapshotCache) GetSnapshot(context.Context, SchedulerBucket) ([]*Account, bool, error) {
	return c.snapshot, c.hit, nil
}

func (c *dailyCooldownSnapshotCache) CaptureBucketWriteToken(_ context.Context, bucket SchedulerBucket) (SchedulerBucketWriteToken, error) {
	return SchedulerBucketWriteToken{Bucket: bucket, Epoch: 1}, nil
}

func (c *dailyCooldownSnapshotCache) SetSnapshot(_ context.Context, _ SchedulerBucket, _ SchedulerBucketWriteToken, accounts []Account) error {
	c.snapshot = make([]*Account, len(accounts))
	for i := range accounts {
		copy := accounts[i]
		c.snapshot[i] = &copy
	}
	c.hit = true
	return nil
}

func TestDailyCooldownSnapshotRetainsAccountUntilWakeup(t *testing.T) {
	account := Account{
		ID: 19, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true,
		Extra: dailyCooldownExtra("23:00", "08:00", "Asia/Shanghai"),
	}
	key := batchAccountQueryKey{platform: PlatformOpenAI}
	repo := newBatchAccountQueryRepo()
	repo.results[key] = []batchAccountQueryResult{{accounts: []Account{account}}}
	cache := &dailyCooldownSnapshotCache{}
	svc := NewSchedulerSnapshotService(cache, nil, repo, nil, nil)
	start := time.Date(2026, 9, 19, 23, 0, 0, 0, time.FixedZone("CST", 8*3600))
	end := start.Add(9 * time.Hour)
	for _, now := range []time.Time{start, end} {
		candidates, _, err := svc.ListSchedulableAccounts(context.Background(), nil, PlatformOpenAI, false)
		require.NoError(t, err)
		require.Len(t, candidates, 1, "cached candidate must survive the cooling interval")
		require.Equal(t, now.Equal(end), candidates[0].isSchedulableAt(now))
	}
	require.Equal(t, 1, repo.callCount(key), "wakeup must not require a database refresh or timer")
	require.True(t, cache.snapshot[0].Schedulable)
	require.Equal(t, StatusActive, cache.snapshot[0].Status)
}

func TestDailyCooldownSnapshotRebuildRetainsMixedCandidates(t *testing.T) {
	repo := newBatchAccountQueryRepo()
	key := batchAccountQueryKey{platform: PlatformAnthropic, mixed: true}
	accounts := []Account{
		{ID: 1, Platform: PlatformAnthropic, Status: StatusActive, Schedulable: true, Extra: dailyCooldownExtra("23:00", "08:00", "Asia/Shanghai")},
		{ID: 2, Platform: PlatformAntigravity, Status: StatusActive, Schedulable: true, Extra: dailyCooldownExtra("23:00", "08:00", "Asia/Shanghai")},
	}
	accounts[1].Extra["mixed_scheduling"] = true
	repo.results[key] = []batchAccountQueryResult{{accounts: accounts}}
	svc := NewSchedulerSnapshotService(nil, nil, repo, nil, nil)
	bucket := SchedulerBucket{Platform: PlatformAnthropic, Mode: SchedulerModeMixed}
	loaded, err := svc.loadAccountsForRebuild(context.Background(), bucket, nil)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	start := time.Date(2026, 9, 19, 23, 0, 0, 0, time.FixedZone("CST", 8*3600))
	for _, account := range loaded {
		require.False(t, account.isSchedulableAt(start))
		require.True(t, account.isSchedulableAt(start.Add(9*time.Hour)))
	}
}

func TestDailyCooldownSchedulerSkipsStickyAccount(t *testing.T) {
	groupID := int64(29)
	now := time.Now().UTC()
	primary := Account{ID: 31, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1, GroupIDs: []int64{groupID}}
	backup := Account{ID: 32, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 5, GroupIDs: []int64{groupID}}
	cooling := primary
	cooling.Extra = dailyCooldownExtra(now.Add(-time.Hour).Format("15:04"), now.Add(time.Hour).Format("15:04"), "UTC")
	snapshotCache := &openAISnapshotCacheStub{
		snapshotAccounts: []*Account{&primary, &backup},
		accountsByID:     map[int64]*Account{31: &cooling, 32: &backup},
	}
	svc := &OpenAIGatewayService{
		accountRepo:        schedulerTestOpenAIAccountRepo{accounts: []Account{cooling, backup}},
		cache:              &schedulerTestGatewayCache{sessionBindings: map[string]int64{"openai:cooldown_session": 31}},
		cfg:                &config.Config{},
		rateLimitService:   newOpenAIAdvancedSchedulerRateLimitService("true"),
		schedulerSnapshot:  &SchedulerSnapshotService{cache: snapshotCache},
		concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{}),
	}
	selection, decision, err := svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "cooldown_session", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, int64(32), selection.Account.ID)
	require.Equal(t, openAIAccountScheduleLayerLoadBalance, decision.Layer)
}
