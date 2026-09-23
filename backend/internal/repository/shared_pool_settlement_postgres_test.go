//go:build sharedpoolintegration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func independentSharedSettlementFixture(t *testing.T) (*sql.DB, *service.UsageBillingCommand) {
	t.Helper()
	db, cmd := sharedPoolPostgresFixture(t)
	_, err := db.Exec(`UPDATE groups SET is_shared_pool = FALSE WHERE id = 5;
		UPDATE shared_pool_settings SET platform_rate_bps = 7000, proxy_rate_bps = 2000, settlement_multiplier = 2;
		INSERT INTO proxies(id) VALUES (9)`)
	require.NoError(t, err)
	multiplier, platformBPS, proxyBPS, proxyID := .5, 500, 100, int64(9)
	cmd.SharedPoolGroup = false
	cmd.SharedPoolBaseCost, cmd.SharedPoolSettlementMultiplier = 10, &multiplier
	cmd.SharedPoolPlatformRateBPS, cmd.SharedPoolProxyRateBPS = &platformBPS, &proxyBPS
	cmd.UsesPlatformProxy, cmd.SharedPoolProxyID = true, &proxyID
	return db, cmd
}

func TestSharedPoolPostgresSettlementFixedOwnerAndSnapshots(t *testing.T) {
	for _, paid := range []float64{5, 10, 15} {
		t.Run(fmt.Sprintf("paid_%g", paid), func(t *testing.T) {
			db, cmd := independentSharedSettlementFixture(t)
			cmd.BalanceCost = paid
			result, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
			require.NoError(t, err)
			require.True(t, result.Applied)
			page, err := NewSharedPoolEarningsRepository(db).List(context.Background(), 6, service.SharedPoolEarningsFilter{})
			require.NoError(t, err)
			require.EqualValues(t, 1, page.Total)
			require.Len(t, page.Items, 1)
			entry := page.Items[0]
			require.Equal(t, paid, entry.BillingAmount)
			require.Equal(t, 4.7, entry.OwnerAmount)
			require.InDelta(t, 0.3, entry.PlatformAmount, 1e-8)
			require.Equal(t, 500, entry.PlatformRateBPS)
			require.Equal(t, 100, entry.ProxyRateBPS)
			require.Equal(t, "available", entry.Status)
			require.NotNil(t, entry.BaseAmount)
			require.NotNil(t, entry.SettlementMultiplier)
			require.NotNil(t, entry.SettlementAmount)
			require.NotNil(t, entry.SpreadAmount)
			require.Equal(t, 10.0, *entry.BaseAmount)
			require.Equal(t, .5, *entry.SettlementMultiplier)
			require.Equal(t, 5.0, *entry.SettlementAmount)
			require.Equal(t, paid-5, *entry.SpreadAmount)
			var storedPlatform string
			require.NoError(t, db.QueryRow(`SELECT platform_amount::text FROM shared_pool_earnings`).Scan(&storedPlatform))
			require.Equal(t, fmt.Sprintf("%.8f", paid-4.7), storedPlatform)
			summary, err := NewSharedPoolEarningsRepository(db).Summary(context.Background(), 6)
			require.NoError(t, err)
			require.InDelta(t, 0.3, summary.PlatformAmount, 1e-8)
			requireSettlementConsumerBalance(t, db, 100-paid)
		})
	}
}

func TestSharedPoolPostgresSettlementUnderpaymentRollsBackAllEffects(t *testing.T) {
	for _, paid := range []float64{0, 4.99999999} {
		t.Run(fmt.Sprintf("paid_%g", paid), func(t *testing.T) {
			db, cmd := independentSharedSettlementFixture(t)
			cmd.BalanceCost = paid
			repo := NewUsageBillingRepository(nil, db)
			_, err := repo.Apply(context.Background(), cmd)
			require.ErrorIs(t, err, service.ErrSharedPoolBillingInvalid)
			requireSettlementConsumerBalance(t, db, 100)
			var earnings, dedup int
			require.NoError(t, db.QueryRow(`SELECT (SELECT COUNT(*) FROM shared_pool_earnings), (SELECT COUNT(*) FROM usage_billing_dedup)`).Scan(&earnings, &dedup))
			require.Zero(t, earnings)
			require.Zero(t, dedup)
			cmd.BalanceCost, cmd.RequestFingerprint = 5, ""
			result, err := repo.Apply(context.Background(), cmd)
			require.NoError(t, err)
			require.True(t, result.Applied, "被回滚的请求不得占用幂等键")
			requireSettlementConsumerBalance(t, db, 95)
		})
	}
}

func TestSharedPoolPostgresSettlementConcurrentReplayIsIdempotent(t *testing.T) {
	db, cmd := independentSharedSettlementFixture(t)
	repo := NewUsageBillingRepository(nil, db)
	for _, err := range sharedPoolRunConcurrent(func() error { copy := *cmd; _, err := repo.Apply(context.Background(), &copy); return err }) {
		require.NoError(t, err)
	}
	_, err := db.Exec(`UPDATE shared_pool_settings SET platform_rate_bps=9000, proxy_rate_bps=0, settlement_multiplier=10`)
	require.NoError(t, err)
	result, err := repo.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.False(t, result.Applied)
	requireSettlementConsumerBalance(t, db, 95)
	var count int
	var owner string
	require.NoError(t, db.QueryRow(`SELECT COUNT(*), SUM(owner_amount)::text FROM shared_pool_earnings`).Scan(&count, &owner))
	require.Equal(t, 1, count)
	require.Equal(t, "4.70000000", owner)
	changed := *cmd
	changed.RequestFingerprint = ""
	changedFee := 600
	changed.SharedPoolPlatformRateBPS = &changedFee
	_, err = repo.Apply(context.Background(), &changed)
	require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
	requireSettlementConsumerBalance(t, db, 95)
}

func TestSharedPoolPostgresSettlementZeroAndCustomProxy(t *testing.T) {
	for _, zeroMultiplier := range []bool{false, true} {
		t.Run(fmt.Sprintf("zero_%t", zeroMultiplier), func(t *testing.T) {
			db, cmd := independentSharedSettlementFixture(t)
			cmd.UsesPlatformProxy, cmd.SharedPoolProxyID = false, nil
			if zeroMultiplier {
				zero := 0.0
				cmd.SharedPoolSettlementMultiplier = &zero
			}
			_, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
			require.NoError(t, err)
			var owner, platform, amount string
			var proxyBPS int
			require.NoError(t, db.QueryRow(`SELECT owner_amount::text, platform_amount::text, settlement_amount::text, proxy_rate_bps FROM shared_pool_earnings`).Scan(&owner, &platform, &amount, &proxyBPS))
			require.Zero(t, proxyBPS)
			if zeroMultiplier {
				require.Equal(t, "0.00000000", owner)
				require.Equal(t, "5.00000000", platform)
				require.Equal(t, "0.00000000", amount)
			} else {
				require.Equal(t, "4.75000000", owner)
				require.Equal(t, "0.25000000", platform)
				require.Equal(t, "5.00000000", amount)
			}
		})
	}
}

func TestSharedPoolPostgresSettlementLegacyListKeepsNullSnapshots(t *testing.T) {
	db, cmd := sharedPoolPostgresFixture(t)
	_, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.NoError(t, err)
	page, err := NewSharedPoolEarningsRepository(db).List(context.Background(), 6, service.SharedPoolEarningsFilter{})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, 4.0, page.Items[0].OwnerAmount)
	require.Nil(t, page.Items[0].BaseAmount)
	require.Nil(t, page.Items[0].SettlementMultiplier)
	require.Nil(t, page.Items[0].SettlementAmount)
	require.Nil(t, page.Items[0].SpreadAmount)
}

func requireSettlementConsumerBalance(t *testing.T, db *sql.DB, expected float64) {
	t.Helper()
	var balance float64
	require.NoError(t, db.QueryRow(`SELECT balance FROM users WHERE id=3`).Scan(&balance))
	require.Equal(t, expected, balance)
}
