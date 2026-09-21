package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type sharedPoolConcurrencyCache struct {
	ConcurrencyCache
	counts  map[int64]int
	err     error
	queries [][]int64
}

func (c *sharedPoolConcurrencyCache) GetAccountConcurrencyBatch(_ context.Context, ids []int64) (map[int64]int, error) {
	c.queries = append(c.queries, append([]int64(nil), ids...))
	return c.counts, c.err
}

func concurrencyPool(id int64, ids ...int64) Group {
	return Group{ID: id, Platform: PlatformOpenAI, IsSharedPool: true, SharedPoolCapacity: &SharedPoolCapacity{
		AvailableAccounts: int64(len(ids)), AvailableAccountIDs: ids, ConcurrencyCapacity: int64(len(ids)) * 3,
	}}
}

func TestSharedPoolCurrentConcurrencyBatchesSharedMembers(t *testing.T) {
	cache := &sharedPoolConcurrencyCache{counts: map[int64]int{1: 8, 2: 0, 3: 2}}
	svc := &APIKeyService{concurrencyService: NewConcurrencyService(cache)}
	groups := []Group{concurrencyPool(10, 1, 2), concurrencyPool(20, 1, 3), concurrencyPool(30, 2), concurrencyPool(40)}
	groups[1].Platform = PlatformGemini
	groups[1].IsSharedPool = false
	got := svc.SharedPoolCurrentConcurrency(context.Background(), groups, config.OpenAICodexTicketConfig{}, time.Now())
	require.Equal(t, map[int64]int64{10: 8, 20: 10, 30: 0, 40: 0}, got, "真实占用允许超过新调低的容量")
	require.Len(t, cache.queries, 1)
	require.ElementsMatch(t, []int64{1, 2, 3}, cache.queries[0], "跨池共用账号只查询一次")
}

func TestSharedPoolCurrentConcurrencyUnavailableCounts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cache *sharedPoolConcurrencyCache
	}{
		{"cache error", &sharedPoolConcurrencyCache{err: errors.New("unavailable")}},
		{"missing account", &sharedPoolConcurrencyCache{counts: map[int64]int{1: 1}}},
		{"negative count", &sharedPoolConcurrencyCache{counts: map[int64]int{1: 1, 2: -1}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &APIKeyService{concurrencyService: NewConcurrencyService(tc.cache)}
			got := svc.SharedPoolCurrentConcurrency(context.Background(), []Group{concurrencyPool(10, 1, 2), concurrencyPool(20)}, config.OpenAICodexTicketConfig{}, time.Now())
			require.Equal(t, map[int64]int64{20: 0}, got)
		})
	}
	for _, svc := range []*APIKeyService{nil, {}, {concurrencyService: NewConcurrencyService(nil)}} {
		got := svc.SharedPoolCurrentConcurrency(context.Background(), []Group{concurrencyPool(10, 1), concurrencyPool(20)}, config.OpenAICodexTicketConfig{}, time.Now())
		require.Equal(t, map[int64]int64{20: 0}, got, "缺少依赖不能伪造有账号池的零占用")
	}
}

func TestSharedPoolCurrentConcurrencySkipsUntrackablePools(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Group)
	}{
		{"legacy no capacity", func(g *Group) { g.SharedPoolCapacity = nil }},
		{"legacy no members", func(g *Group) { g.SharedPoolCapacity.AvailableAccountIDs = nil }},
		{"unlimited", func(g *Group) { g.SharedPoolCapacity.ConcurrencyUnlimited = true }},
		{"protected raw zero concurrency", func(g *Group) {
			g.SharedPoolCapacity.UntrackedConcurrencyAccountIDs = []int64{1}
			g.SharedPoolCapacity.ConcurrencyCapacity = 16
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := concurrencyPool(10, 1)
			tc.change(&group)
			cache := &sharedPoolConcurrencyCache{counts: map[int64]int{1: 0}}
			svc := &APIKeyService{concurrencyService: NewConcurrencyService(cache)}
			require.Empty(t, svc.SharedPoolCurrentConcurrency(context.Background(), []Group{group}, config.OpenAICodexTicketConfig{}, time.Now()))
			require.Empty(t, cache.queries)
		})
	}
}

func TestSharedPoolCurrentConcurrencyUsesSameTicketMembers(t *testing.T) {
	now := sharedTicketProgressNow
	ready := SharedPoolTicketAccountSnapshot{AccountID: 1, Available: true, Concurrency: 3,
		tickets: map[string]sharedPoolTicketMetadata{"ready": {length: 292, expiresAt: now.Add(time.Second)}}}
	waiting := SharedPoolTicketAccountSnapshot{AccountID: 2, Available: true, Concurrency: 3}
	group := concurrencyPool(10, 1, 2, 3)
	group.SharedPoolCapacity.TicketAccounts = []SharedPoolTicketAccountSnapshot{ready, waiting}
	cache := &sharedPoolConcurrencyCache{counts: map[int64]int{1: 2, 2: 50, 3: 1}}
	svc := &APIKeyService{concurrencyService: NewConcurrencyService(cache)}
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: []string{"missing", "ready"}}
	require.Equal(t, map[int64]int64{10: 3}, svc.SharedPoolCurrentConcurrency(context.Background(), []Group{group}, cfg, now))
	require.ElementsMatch(t, []int64{1, 3}, cache.queries[0], "任一模型就绪保留；无票排除；非打票账号保留")
	require.Equal(t, []int64{1, 2, 3}, group.SharedPoolCapacity.AvailableAccountIDs, "来源成员不能被原地重写")
	require.EqualValues(t, 3, group.SharedPoolCapacity.AvailableAccounts)
	cfg.FailClosed = false
	require.Equal(t, map[int64]int64{10: 53}, svc.SharedPoolCurrentConcurrency(context.Background(), []Group{group}, cfg, now))
}

func TestSharedPoolCurrentConcurrencyExcludesUntrackedWaitingAccount(t *testing.T) {
	for _, protected := range []bool{false, true} {
		group := concurrencyPool(10, 1, 2)
		capacity := group.SharedPoolCapacity
		capacity.UntrackedConcurrencyAccountIDs = []int64{2}
		waiting := SharedPoolTicketAccountSnapshot{AccountID: 2, Available: true}
		capacity.ConcurrencyCapacity = 3
		if protected {
			waiting.Concurrency = 16
			capacity.ConcurrencyCapacity += 16
		} else {
			capacity.UnlimitedAccounts = 1
			capacity.ConcurrencyUnlimited = true
		}
		capacity.TicketAccounts = []SharedPoolTicketAccountSnapshot{waiting}
		cache := &sharedPoolConcurrencyCache{counts: map[int64]int{1: 2}}
		svc := &APIKeyService{concurrencyService: NewConcurrencyService(cache)}
		cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
		require.Equal(t, map[int64]int64{10: 2}, svc.SharedPoolCurrentConcurrency(context.Background(), []Group{group}, cfg, sharedTicketProgressNow))
		require.Equal(t, [][]int64{{1}}, cache.queries)
		require.Equal(t, []int64{2}, capacity.UntrackedConcurrencyAccountIDs)
	}
}

func TestSharedPoolCurrentConcurrencyAllWaitingReturnsZero(t *testing.T) {
	group := concurrencyPool(10, 1)
	group.SharedPoolCapacity.TicketAccounts = []SharedPoolTicketAccountSnapshot{{AccountID: 1, Available: true, Concurrency: 3}}
	cache := &sharedPoolConcurrencyCache{}
	svc := &APIKeyService{concurrencyService: NewConcurrencyService(cache)}
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
	require.Equal(t, map[int64]int64{10: 0}, svc.SharedPoolCurrentConcurrency(context.Background(), []Group{group}, cfg, sharedTicketProgressNow))
	require.Empty(t, cache.queries)
}
