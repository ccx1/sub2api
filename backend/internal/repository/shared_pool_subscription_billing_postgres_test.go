//go:build sharedpoolintegration

package repository

import (
	"context"
	"database/sql"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func sharedSubscriptionPostgresFixture(t *testing.T) (*sql.DB, *service.UsageBillingCommand) {
	t.Helper()
	db, cmd := independentSharedSettlementFixture(t)
	_, err := db.Exec(`ALTER TABLE groups ADD COLUMN deleted_at TIMESTAMPTZ;
		UPDATE groups SET subscription_type='subscription' WHERE id=5;
		CREATE TABLE user_subscriptions (
			id BIGINT PRIMARY KEY, user_id BIGINT NOT NULL, group_id BIGINT NOT NULL,
			daily_usage_usd NUMERIC(20,8) NOT NULL DEFAULT 0,
			weekly_usage_usd NUMERIC(20,8) NOT NULL DEFAULT 0,
			monthly_usage_usd NUMERIC(20,8) NOT NULL DEFAULT 0,
			deleted_at TIMESTAMPTZ, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
		INSERT INTO user_subscriptions(id,user_id,group_id) VALUES(17,3,5)`)
	require.NoError(t, err)
	cmd.SubscriptionID, cmd.SubscriptionCost, cmd.BalanceCost = new(int64(17)), 10, 0
	return db, cmd
}

func requireSharedSubscriptionUsage(t *testing.T, db *sql.DB, expected float64) {
	t.Helper()
	var daily, weekly, monthly float64
	require.NoError(t, db.QueryRow(`SELECT daily_usage_usd,weekly_usage_usd,monthly_usage_usd FROM user_subscriptions WHERE id=17`).Scan(&daily, &weekly, &monthly))
	require.Equal(t, expected, daily)
	require.Equal(t, expected, weekly)
	require.Equal(t, expected, monthly)
	requireSettlementConsumerBalance(t, db, 100)
}

func TestSharedPoolSubscriptionPostgresSettlementAndConcurrentReplay(t *testing.T) {
	db, cmd := sharedSubscriptionPostgresFixture(t)
	ctx, repo := context.Background(), NewUsageBillingRepository(nil, db)
	for _, err := range sharedPoolRunConcurrent(func() error {
		copy := *cmd
		_, err := repo.Apply(ctx, &copy)
		return err
	}) {
		require.NoError(t, err)
	}
	requireSharedSubscriptionUsage(t, db, 10)
	page, err := NewSharedPoolEarningsRepository(db).List(ctx, 6, service.SharedPoolEarningsFilter{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	entry := page.Items[0]
	require.Equal(t, 10.0, entry.BillingAmount)
	require.Equal(t, 4.7, entry.OwnerAmount)
	require.Equal(t, 0.3, entry.PlatformAmount)
	require.Equal(t, 5.0, *entry.SettlementAmount)
	require.Equal(t, 5.0, *entry.SpreadAmount)
	require.Equal(t, "available", entry.Status)
	result, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.False(t, result.Applied)
	requireSharedSubscriptionUsage(t, db, 10)
}

func TestSharedPoolSubscriptionPostgresLedgerFailureRollsBackQuotaAndDedup(t *testing.T) {
	db, cmd := sharedSubscriptionPostgresFixture(t)
	_, err := db.Exec(`ALTER TABLE shared_pool_earnings ADD CONSTRAINT reject_fixture_earning CHECK(owner_amount=0)`)
	require.NoError(t, err)
	ctx, repo := context.Background(), NewUsageBillingRepository(nil, db)
	_, err = repo.Apply(ctx, cmd)
	require.Error(t, err)
	requireSharedSubscriptionUsage(t, db, 0)
	var earnings, dedup int
	require.NoError(t, db.QueryRow(`SELECT (SELECT COUNT(*) FROM shared_pool_earnings),(SELECT COUNT(*) FROM usage_billing_dedup)`).Scan(&earnings, &dedup))
	require.Zero(t, earnings)
	require.Zero(t, dedup)
	_, err = db.Exec(`ALTER TABLE shared_pool_earnings DROP CONSTRAINT reject_fixture_earning`)
	require.NoError(t, err)
	result, err := repo.Apply(ctx, cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	requireSharedSubscriptionUsage(t, db, 10)
}

func TestSharedPoolSubscriptionPostgresInvalidSourcesRollBack(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*service.UsageBillingCommand)
		want   error
	}{
		{"mixed costs", func(c *service.UsageBillingCommand) { c.BalanceCost = 5 }, service.ErrSharedPoolBillingInvalid},
		{"missing subscription ID", func(c *service.UsageBillingCommand) { c.SubscriptionID = nil }, service.ErrSharedPoolBillingInvalid},
		{"unknown subscription ID", func(c *service.UsageBillingCommand) { c.SubscriptionID = new(int64(88)) }, service.ErrSubscriptionNotFound},
		{"underfunded quota cost", func(c *service.UsageBillingCommand) { c.SubscriptionCost = 4.99999999 }, service.ErrSharedPoolBillingInvalid},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, cmd := sharedSubscriptionPostgresFixture(t)
			tc.change(cmd)
			_, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
			require.ErrorIs(t, err, tc.want)
			requireSharedSubscriptionUsage(t, db, 0)
			var earnings, dedup int
			require.NoError(t, db.QueryRow(`SELECT (SELECT COUNT(*) FROM shared_pool_earnings),(SELECT COUNT(*) FROM usage_billing_dedup)`).Scan(&earnings, &dedup))
			require.Zero(t, earnings)
			require.Zero(t, dedup)
		})
	}
}

func TestSharedPoolSubscriptionPostgresSelfAndLegacyEarnNothing(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		db, cmd := sharedSubscriptionPostgresFixture(t)
		if legacy {
			cmd.SharedPoolSettlementMultiplier = nil
			cmd.SharedPoolGroup = true
		} else {
			cmd.SharedPoolOwnerID = cmd.UserID
		}
		result, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
		require.NoError(t, err)
		require.True(t, result.Applied)
		requireSharedSubscriptionUsage(t, db, 10)
		var earnings int
		require.NoError(t, db.QueryRow(`SELECT COUNT(*) FROM shared_pool_earnings`).Scan(&earnings))
		require.Zero(t, earnings)
	}
}
