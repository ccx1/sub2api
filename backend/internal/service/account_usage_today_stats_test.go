package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
)

type todayStatsWindowRepoStub struct {
	usageBatchLogRepoStub
	today       map[int64]*usagestats.AccountStats
	lifetime    map[int64]*usagestats.AccountStats
	lifetimeErr error
}

func (r *todayStatsWindowRepoStub) GetAccountTodayStats(_ context.Context, accountID int64) (*usagestats.AccountStats, error) {
	if stats, ok := r.today[accountID]; ok {
		return stats, nil
	}
	return &usagestats.AccountStats{}, nil
}

func (r *todayStatsWindowRepoStub) GetAccountWindowStats(_ context.Context, accountID int64, startTime time.Time) (*usagestats.AccountStats, error) {
	source := r.today
	if startTime.IsZero() {
		if r.lifetimeErr != nil {
			return nil, r.lifetimeErr
		}
		source = r.lifetime
	}
	if stats, ok := source[accountID]; ok {
		return stats, nil
	}
	return &usagestats.AccountStats{}, nil
}

func (r *todayStatsWindowRepoStub) GetAccountWindowStatsBatch(_ context.Context, accountIDs []int64, startTime time.Time) (map[int64]*usagestats.AccountStats, error) {
	out := make(map[int64]*usagestats.AccountStats, len(accountIDs))
	for _, id := range accountIDs {
		stats, err := r.GetAccountWindowStats(context.Background(), id, startTime)
		if err != nil {
			return nil, err
		}
		out[id] = stats
	}
	return out, nil
}

func TestGetTodayStatsIncludesLifetimeTotals(t *testing.T) {
	t.Parallel()
	repo := &todayStatsWindowRepoStub{
		today: map[int64]*usagestats.AccountStats{
			3: {Requests: 10, Tokens: 1000, Cost: 1.25, StandardCost: 1.25, UserCost: 1.25},
		},
		lifetime: map[int64]*usagestats.AccountStats{
			3: {Requests: 50, Tokens: 800000000, Cost: 1904.56, StandardCost: 1904.56, UserCost: 1904.56},
		},
	}
	svc := &AccountUsageService{usageLogRepo: repo}

	got, err := svc.GetTodayStats(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetTodayStats: %v", err)
	}
	if got.Tokens != 1000 || got.Cost != 1.25 {
		t.Fatalf("today stats = tokens %d cost %v, want 1000 / 1.25", got.Tokens, got.Cost)
	}
	if got.LifetimeTokens == nil || *got.LifetimeTokens != 800000000 || got.LifetimeCost == nil || *got.LifetimeCost != 1904.56 {
		t.Fatalf("lifetime stats = tokens %d cost %v, want 800000000 / 1904.56", got.LifetimeTokens, got.LifetimeCost)
	}
}

func TestGetTodayStatsBatchIncludesLifetimeTotals(t *testing.T) {
	t.Parallel()
	repo := &todayStatsWindowRepoStub{
		today: map[int64]*usagestats.AccountStats{
			3: {Tokens: 200, Cost: 2},
			8: {Tokens: 0, Cost: 0},
		},
		lifetime: map[int64]*usagestats.AccountStats{
			3: {Tokens: 900, Cost: 9.5},
			8: {Tokens: 12, Cost: 0.4},
		},
	}
	svc := &AccountUsageService{usageLogRepo: repo}

	got, err := svc.GetTodayStatsBatch(context.Background(), []int64{3, 8, 3})
	if err != nil {
		t.Fatalf("GetTodayStatsBatch: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d accounts, want 2", len(got))
	}
	if got[3].Tokens != 200 || got[3].LifetimeTokens == nil || *got[3].LifetimeTokens != 900 || got[3].LifetimeCost == nil || *got[3].LifetimeCost != 9.5 {
		t.Fatalf("account 3 = %+v", got[3])
	}
	if got[8].LifetimeTokens == nil || *got[8].LifetimeTokens != 12 || got[8].LifetimeCost == nil || *got[8].LifetimeCost != 0.4 {
		t.Fatalf("account 8 = %+v", got[8])
	}
}

func TestGetTodayStatsSerializesSuccessfulZeroLifetime(t *testing.T) {
	for _, mode := range []string{"single", "batch"} {
		t.Run(mode, func(t *testing.T) {
			repo := todayStatsJSONRepo()
			got := readTodayStatsForJSON(t, repo, mode)
			assertTodayStatsJSON(t, got, true)
		})
	}
}

func TestGetTodayStatsOmitsFailedLifetimeLookup(t *testing.T) {
	for _, mode := range []string{"single", "batch"} {
		t.Run(mode, func(t *testing.T) {
			repo := todayStatsJSONRepo()
			repo.lifetimeErr = errors.New("lifetime lookup failed")
			got := readTodayStatsForJSON(t, repo, mode)
			assertTodayStatsJSON(t, got, false)
		})
	}
}

func TestAccountWindowStatsOmitsLifetimeOutsideTodayQueries(t *testing.T) {
	repo := todayStatsJSONRepo()
	got := windowStatsFromAccountStats(repo.today[3])
	assertTodayStatsJSON(t, got, false)
}

func todayStatsJSONRepo() *todayStatsWindowRepoStub {
	return &todayStatsWindowRepoStub{
		today: map[int64]*usagestats.AccountStats{
			3: {Requests: 10, Tokens: 1000, Cost: 1.25, StandardCost: 2.5, UserCost: 3.75},
		},
		lifetime: map[int64]*usagestats.AccountStats{3: {}},
	}
}

func readTodayStatsForJSON(t *testing.T, repo *todayStatsWindowRepoStub, mode string) *WindowStats {
	t.Helper()
	svc := &AccountUsageService{usageLogRepo: repo}
	if mode == "batch" {
		stats, err := svc.GetTodayStatsBatch(context.Background(), []int64{3})
		if err != nil {
			t.Fatalf("GetTodayStatsBatch: %v", err)
		}
		return stats[3]
	}
	stats, err := svc.GetTodayStats(context.Background(), 3)
	if err != nil {
		t.Fatalf("GetTodayStats: %v", err)
	}
	return stats
}

func assertTodayStatsJSON(t *testing.T, stats *WindowStats, hasLifetime bool) {
	t.Helper()
	data, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal window stats: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatalf("decode window stats: %v", err)
	}
	for key, want := range map[string]string{
		"requests": "10", "tokens": "1000", "cost": "1.25", "standard_cost": "2.5", "user_cost": "3.75",
	} {
		if string(fields[key]) != want {
			t.Errorf("%s = %s, want %s", key, fields[key], want)
		}
	}
	for _, key := range []string{"lifetime_tokens", "lifetime_cost"} {
		value, exists := fields[key]
		if exists != hasLifetime {
			t.Errorf("%s presence = %v, want %v", key, exists, hasLifetime)
		}
		if hasLifetime && string(value) != "0" {
			t.Errorf("%s = %s, want explicit zero", key, value)
		}
	}
}
