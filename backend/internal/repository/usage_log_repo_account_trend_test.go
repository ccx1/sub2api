package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"modernc.org/sqlite"
)

// 仅适配日期格式函数；排名、分桶和金额聚合直接执行仓库的原始 SQL。
var registerAccountTrendDateFormat = sync.OnceValue(func() error {
	return sqlite.RegisterDeterministicScalarFunction("to_char", 2, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		value := fmt.Sprint(args[0])
		switch args[1] {
		case "YYYY-MM-DD":
			return value[:10], nil
		case "YYYY-MM-DD HH24:00":
			return value[:13] + ":00", nil
		default:
			return nil, fmt.Errorf("unsupported test date format: %v", args[1])
		}
	})
})

func newAccountTrendFixture(t *testing.T) (*usageLogRepository, time.Time) {
	t.Helper()
	require.NoError(t, registerAccountTrendDateFormat())
	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.Exec(`
		CREATE TABLE accounts (id INTEGER PRIMARY KEY, name TEXT, type TEXT, platform TEXT);
		CREATE TABLE usage_logs (
			created_at TIMESTAMP, account_id INTEGER, user_id INTEGER DEFAULT 1,
			api_key_id INTEGER DEFAULT 2, group_id INTEGER DEFAULT 7, model TEXT DEFAULT 'test-model',
			request_type INTEGER DEFAULT 1, stream BOOLEAN DEFAULT FALSE, openai_ws_mode BOOLEAN DEFAULT FALSE,
			billing_type INTEGER DEFAULT 0, input_tokens INTEGER, output_tokens INTEGER,
			cache_creation_tokens INTEGER, cache_read_tokens INTEGER, total_cost REAL,
			actual_cost REAL, account_stats_cost REAL, account_rate_multiplier REAL
		)`)
	require.NoError(t, err)
	start := time.Date(2026, 9, 19, 0, 0, 0, 0, time.UTC)
	seedAccountTrendFixture(t, db, start)
	return &usageLogRepository{sql: db}, start
}

func seedAccountTrendFixture(t *testing.T, db *sql.DB, start time.Time) {
	t.Helper()
	for id := 1; id <= 13; id++ {
		_, err := db.Exec("INSERT INTO accounts VALUES (?, ?, 'oauth', 'openai')", id, fmt.Sprintf("account-%d", id))
		require.NoError(t, err)
	}
	insert := `INSERT INTO usage_logs (created_at, account_id, user_id, input_tokens, output_tokens,
		cache_creation_tokens, cache_read_tokens, total_cost, actual_cost, account_stats_cost, account_rate_multiplier)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for id := 1; id <= 10; id++ {
		_, err := db.Exec(insert, start.Add(15*time.Hour), id, 1, 1000, 100, 10, 20, 10, 5, 8, 0.5)
		require.NoError(t, err)
	}
	rows := [][]any{
		{start.Add(15 * time.Hour), 11, 1, 20, 0, 0, 0, 0.5, 0.25, 0.125, 2},
		{start.Add(16 * time.Hour), 11, 1, 10, 20, 30, 40, 1, 0.5, 0.75, 2},
		{start.Add(16 * time.Hour), 12, 1, 5, 10, 15, 20, 2, 1, nil, nil},
		{start.Add(16 * time.Hour), 13, 2, 1000000, 0, 0, 0, 1000, 500, nil, nil},
		{start.Add(24 * time.Hour), 13, 1, 1000000, 0, 0, 0, 1000, 500, nil, nil},
		{start.Add(-time.Hour), 13, 1, 1000000, 0, 0, 0, 1000, 500, nil, nil},
		{start.Add(16 * time.Hour), 0, 1, 1000000, 0, 0, 0, 1000, 500, nil, nil},
	}
	for _, row := range rows {
		_, err := db.Exec(insert, row...)
		require.NoError(t, err)
	}
}

func accountTrendTotals(points []AccountUsageTrendPoint) AccountUsageTrendPoint {
	var total AccountUsageTrendPoint
	for _, point := range points {
		total.Requests += point.Requests
		total.Tokens += point.Tokens
		total.Cost += point.Cost
		total.ActualCost += point.ActualCost
		total.AccountCost += point.AccountCost
	}
	return total
}

func TestAccountUsageTrendIncludesOtherAccounts(t *testing.T) {
	repo, start := newAccountTrendFixture(t)
	for _, granularity := range []string{"hour", "day"} {
		t.Run(granularity, func(t *testing.T) {
			points, err := repo.GetAccountUsageTrendWithFilters(context.Background(), start, start.AddDate(0, 0, 1), granularity, 1, 0, 0, 0, "", "", "", nil, nil, nil, 0)
			require.NoError(t, err)
			accounts := map[int64]bool{}
			var other []AccountUsageTrendPoint
			for _, point := range points {
				accounts[point.AccountID] = true
				if point.AccountID == 0 {
					other = append(other, point)
					require.Empty(t, point.AccountName)
				}
			}
			require.Len(t, accounts, 11)
			for id := int64(1); id <= 10; id++ {
				require.True(t, accounts[id])
			}
			require.Equal(t, AccountUsageTrendPoint{Requests: 13, Tokens: 11470, Cost: 103.5, ActualCost: 51.75, AccountCost: 43.75}, accountTrendTotals(points))
			require.Equal(t, AccountUsageTrendPoint{Requests: 3, Tokens: 170, Cost: 3.5, ActualCost: 1.75, AccountCost: 3.75}, accountTrendTotals(other))
			if granularity == "hour" {
				require.Len(t, other, 2)
				require.Equal(t, AccountUsageTrendPoint{Date: "2026-09-19 16:00", Requests: 2, Tokens: 150, Cost: 3, ActualCost: 1.5, AccountCost: 3.5}, other[1])
			} else {
				require.Len(t, other, 1)
				require.Equal(t, "2026-09-19", other[0].Date)
			}
		})
	}
}

func TestAccountUsageTrendFiltersAndLimits(t *testing.T) {
	repo, start := newAccountTrendFixture(t)
	cases := []struct {
		name      string
		accountID int64
		limit     int
		series    int
		tokens    int64
	}{
		{name: "all accounts fit", limit: 30, series: 12, tokens: 11470},
		{name: "selected account only", accountID: 11, limit: 1, series: 1, tokens: 120},
		{name: "empty selection", accountID: 999, limit: 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			points, err := repo.GetAccountUsageTrendWithFilters(context.Background(), start, start.AddDate(0, 0, 1), "hour", 1, 2, tc.accountID, 7, "oauth", "openai", "test-model", nil, nil, nil, tc.limit)
			require.NoError(t, err)
			accounts := map[int64]bool{}
			for _, point := range points {
				require.Positive(t, point.AccountID, "must not invent an Other series when no accounts were excluded")
				accounts[point.AccountID] = true
			}
			require.Len(t, accounts, tc.series)
			require.Equal(t, tc.tokens, accountTrendTotals(points).Tokens)
		})
	}
}
