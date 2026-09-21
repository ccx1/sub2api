//go:build sharedpoolintegration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// 只接受本任务创建的本机专用数据库；每次测试使用新 schema，不读取应用配置。
func sharedPoolPostgresFixture(t *testing.T) (*sql.DB, *service.UsageBillingCommand) {
	t.Helper()
	dsn := os.Getenv("SUB2API_SHARED_POOL_TEST_DSN")
	if dsn == "" {
		t.Skip("SUB2API_SHARED_POOL_TEST_DSN is not set")
	}
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", u.Hostname())
	require.Equal(t, "/codex_shared_pool_test", u.Path)
	adminDB, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := fmt.Sprintf("shared_pool_test_%d", time.Now().UnixNano())
	_, err = adminDB.Exec(`CREATE SCHEMA ` + pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := sql.Open("postgres", u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		db.Close()
		_, _ = adminDB.Exec(`DROP SCHEMA ` + pq.QuoteIdentifier(schema) + ` CASCADE`)
		adminDB.Close()
	})
	_, err = db.Exec(`CREATE TABLE users (id BIGINT PRIMARY KEY, balance NUMERIC(20,8) NOT NULL, total_recharged NUMERIC(20,8) NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'active', deleted_at TIMESTAMPTZ, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		CREATE TABLE redeem_codes (id BIGSERIAL PRIMARY KEY, code VARCHAR(32) NOT NULL UNIQUE, type VARCHAR(20) NOT NULL DEFAULT 'balance', value NUMERIC(20,8) NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'unused', used_by BIGINT REFERENCES users(id), used_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			notes TEXT, expires_at TIMESTAMPTZ, group_id BIGINT, validity_days INT NOT NULL DEFAULT 30);
		CREATE TABLE accounts (id BIGINT PRIMARY KEY);
		CREATE TABLE proxies (id BIGINT PRIMARY KEY);
		CREATE TABLE groups (id BIGINT PRIMARY KEY, is_shared_pool BOOLEAN NOT NULL, subscription_type TEXT NOT NULL);
		INSERT INTO users(id, balance) VALUES (3, 100), (6, 10);
		INSERT INTO accounts VALUES (4);
		INSERT INTO groups VALUES (5, TRUE, 'standard');`)
	require.NoError(t, err)
	for _, name := range []string{"071_add_usage_billing_dedup.sql", "073_add_usage_billing_dedup_archive.sql", "250_shared_pool_accounts.sql", "251_shared_pool_earnings.sql", "252_shared_pool_subscription_groups.sql", "253_shared_pool_independent_settlement.sql"} {
		body, err := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		require.NoError(t, err)
		_, err = db.Exec(string(body))
		require.NoError(t, err, "migration %s", name)
	}
	_, err = db.Exec(`INSERT INTO shared_pool_accounts(account_id, owner_user_id, credential_fingerprint) VALUES (4, 6, 'test-credential')`)
	require.NoError(t, err)
	return db, &service.UsageBillingCommand{RequestID: "pool-request", APIKeyID: 2, UserID: 3, AccountID: 4, GroupID: 5, SharedPoolOwnerID: 6, SharedPoolGroup: true, BalanceCost: 5}
}

func sharedPoolRunConcurrent(work func() error) []error {
	var wg sync.WaitGroup
	errs := make([]error, 12)
	for i := range errs {
		wg.Add(1)
		go func(i int) { defer wg.Done(); errs[i] = work() }(i)
	}
	wg.Wait()
	return errs
}

func TestSharedPoolPostgresConcurrentBillingAndTransfer(t *testing.T) {
	db, cmd := sharedPoolPostgresFixture(t)
	ctx := context.Background()
	billing := NewUsageBillingRepository(nil, db)
	for _, err := range sharedPoolRunConcurrent(func() error { copy := *cmd; _, err := billing.Apply(ctx, &copy); return err }) {
		require.NoError(t, err)
	}
	var count int
	var balance, recharged float64
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM shared_pool_earnings`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id = 3`).Scan(&balance))
	require.Equal(t, 95.0, balance)
	earnings := NewSharedPoolEarningsRepository(db)
	successes := 0
	for _, err := range sharedPoolRunConcurrent(func() error { _, err := earnings.Transfer(ctx, 6); return err }) {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, service.ErrSharedPoolEarningsEmpty)
		}
	}
	require.Equal(t, 1, successes)
	require.NoError(t, db.QueryRow(`SELECT balance, total_recharged FROM users WHERE id = 6`).Scan(&balance, &recharged))
	require.Equal(t, 14.0, balance)
	require.Zero(t, recharged)
	summary, err := earnings.Summary(ctx, 6)
	require.NoError(t, err)
	require.Zero(t, summary.Available)
	require.Equal(t, 4.0, summary.Transferred)
}

func TestSharedPoolPostgresRepaymentReleasesPendingOnTransfer(t *testing.T) {
	db, cmd := sharedPoolPostgresFixture(t)
	ctx := context.Background()
	_, err := db.Exec(`UPDATE users SET balance = 1 WHERE id = 3`)
	require.NoError(t, err)
	_, err = NewUsageBillingRepository(nil, db).Apply(ctx, cmd)
	require.NoError(t, err)
	earnings := NewSharedPoolEarningsRepository(db)
	summary, err := earnings.Summary(ctx, 6)
	require.NoError(t, err)
	require.Equal(t, 4.0, summary.Pending)
	require.Zero(t, summary.Available)
	_, err = earnings.Transfer(ctx, 6)
	require.ErrorIs(t, err, service.ErrSharedPoolEarningsEmpty)
	_, err = db.Exec(`UPDATE users SET balance = 0 WHERE id = 3`)
	require.NoError(t, err)
	summary, err = earnings.Summary(ctx, 6)
	require.NoError(t, err)
	require.Zero(t, summary.Pending)
	require.Equal(t, 4.0, summary.Available)
	var status string
	require.NoError(t, db.QueryRow(`SELECT status FROM shared_pool_earnings`).Scan(&status))
	require.Equal(t, "pending", status, "摘要不得隐式更新账本")
	_, err = db.Exec(`UPDATE users SET balance = -1 WHERE id = 3`)
	require.NoError(t, err)
	_, err = earnings.Transfer(ctx, 6)
	require.ErrorIs(t, err, service.ErrSharedPoolEarningsEmpty, "再次欠费时保守等待补足")
	_, err = db.Exec(`UPDATE users SET balance = 0 WHERE id = 3`)
	require.NoError(t, err)
	result, err := earnings.Transfer(ctx, 6)
	require.NoError(t, err)
	require.Equal(t, 4.0, result.Amount)
	var released bool
	require.NoError(t, db.QueryRow(`SELECT released_at IS NOT NULL AND release_reason = 'consumer_balance_restored' FROM shared_pool_earnings`).Scan(&released))
	require.True(t, released)
}

func TestSharedPoolPostgresRollbackAndProxyPrecision(t *testing.T) {
	db, cmd := sharedPoolPostgresFixture(t)
	ctx := context.Background()
	proxyID := int64(9)
	cmd.UsesPlatformProxy, cmd.SharedPoolProxyID = true, &proxyID
	_, err := db.Exec(`INSERT INTO shared_pool_user_rates(user_id, platform_rate_bps, proxy_rate_bps) VALUES (6, 9901, 100)`)
	require.NoError(t, err)
	billing := NewUsageBillingRepository(nil, db)
	_, err = billing.Apply(ctx, cmd)
	require.ErrorIs(t, err, service.ErrSharedPoolBillingInvalid)
	var balance float64
	var count int
	require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id = 3`).Scan(&balance))
	require.Equal(t, 100.0, balance)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup`).Scan(&count))
	require.Zero(t, count)
	_, err = db.Exec(`UPDATE shared_pool_user_rates SET platform_rate_bps = 2000 WHERE user_id = 6`)
	require.NoError(t, err)
	_, err = billing.Apply(ctx, cmd)
	require.NoError(t, err)
	var platform, owner string
	require.NoError(t, db.QueryRow(`SELECT platform_amount::text, owner_amount::text FROM shared_pool_earnings`).Scan(&platform, &owner))
	require.Equal(t, "1.05000000", platform)
	require.Equal(t, "3.95000000", owner)
	cmd = &service.UsageBillingCommand{RequestID: "tiny", APIKeyID: 2, UserID: 3, AccountID: 4, GroupID: 5, SharedPoolOwnerID: 6, SharedPoolGroup: true, BalanceCost: 0.00000005}
	_, err = billing.Apply(ctx, cmd)
	require.NoError(t, err)
	require.NoError(t, db.QueryRow(`SELECT platform_amount::text, owner_amount::text FROM shared_pool_earnings WHERE request_id = 'tiny'`).Scan(&platform, &owner))
	require.Equal(t, "0.00000001", platform)
	require.Equal(t, "0.00000004", owner)
}

func TestSharedPoolPostgresReadIsolationAndDisabledOwner(t *testing.T) {
	db, cmd := sharedPoolPostgresFixture(t)
	ctx := context.Background()
	_, err := NewUsageBillingRepository(nil, db).Apply(ctx, cmd)
	require.NoError(t, err)
	earnings := NewSharedPoolEarningsRepository(db)
	page, err := earnings.List(ctx, 3, service.SharedPoolEarningsFilter{AccountID: 4})
	require.NoError(t, err)
	require.Zero(t, page.Total)
	page, err = earnings.List(ctx, 6, service.SharedPoolEarningsFilter{Status: "available"})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Equal(t, 4.0, page.Items[0].OwnerAmount)
	totals, err := earnings.AccountTotals(ctx, 6, []int64{4})
	require.NoError(t, err)
	require.Equal(t, 4.0, totals[4].TotalEarnings)
	window, err := earnings.AccountWindow(ctx, 6, 4, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.Equal(t, 5.0, window.BillingAmount)
	require.Equal(t, 4.0, window.OwnerAmount)
	_, err = db.Exec(`UPDATE users SET status = 'disabled' WHERE id = 6`)
	require.NoError(t, err)
	_, err = earnings.Transfer(ctx, 6)
	require.ErrorIs(t, err, service.ErrUserNotActive)
	var count int
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM shared_pool_earnings_transfers`).Scan(&count))
	require.Zero(t, count)
}

func TestSharedPoolPostgresInFlightBillingSurvivesGroupChanges(t *testing.T) {
	db, original := sharedPoolPostgresFixture(t)
	ctx := context.Background()
	billing := NewUsageBillingRepository(nil, db)
	for _, groupType := range []string{"standard", "subscription"} {
		_, err := db.Exec(`UPDATE groups SET is_shared_pool = FALSE, subscription_type = $1 WHERE id = 5`, groupType)
		require.NoError(t, err)
		cmd := *original
		cmd.RequestID = "in-flight-" + groupType
		result, err := billing.Apply(ctx, &cmd)
		require.NoError(t, err)
		require.True(t, result.Applied)
	}
	for _, change := range []func(*service.UsageBillingCommand){
		func(cmd *service.UsageBillingCommand) { cmd.SharedPoolGroup = false },
		func(cmd *service.UsageBillingCommand) { cmd.SharedPoolOwnerID = 77 },
		func(cmd *service.UsageBillingCommand) { cmd.GroupID = 99 },
	} {
		cmd := *original
		change(&cmd)
		_, err := billing.Apply(ctx, &cmd)
		require.ErrorIs(t, err, service.ErrSharedPoolBillingInvalid)
	}
	var balance, earned float64
	var count int
	require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id = 3`).Scan(&balance))
	require.Equal(t, 90.0, balance, "在途请求扣费成功，非法快照不影响余额")
	require.NoError(t, db.QueryRow(`SELECT COUNT(*), SUM(owner_amount) FROM shared_pool_earnings`).Scan(&count, &earned))
	require.Equal(t, 2, count)
	require.Equal(t, 8.0, earned)
	require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM usage_billing_dedup`).Scan(&count))
	require.Equal(t, 2, count)
}
