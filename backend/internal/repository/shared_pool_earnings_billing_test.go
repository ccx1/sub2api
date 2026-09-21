//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func sharedPoolTestCommand() *service.UsageBillingCommand {
	return &service.UsageBillingCommand{RequestID: "shared-1", APIKeyID: 2, UserID: 3, AccountID: 4,
		GroupID: 5, SharedPoolOwnerID: 6, SharedPoolGroup: true, BalanceCost: 5}
}

func expectSharedPoolClaim(mock sqlmock.Sqlmock, cmd *service.UsageBillingCommand) {
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO usage_billing_dedup").WithArgs(cmd.RequestID, cmd.APIKeyID, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectQuery("SELECT request_fingerprint.*usage_billing_dedup_archive").
		WithArgs(cmd.RequestID, cmd.APIKeyID).WillReturnError(sql.ErrNoRows)
}

func expectSharedPoolRates(mock sqlmock.Sqlmock, cmd *service.UsageBillingCommand, platformBPS, proxyBPS int) {
	mock.ExpectQuery("SELECT COALESCE\\(ur.platform_rate_bps").WithArgs(cmd.AccountID, cmd.SharedPoolOwnerID, cmd.GroupID).
		WillReturnRows(sqlmock.NewRows([]string{"platform", "proxy"}).AddRow(platformBPS, proxyBPS))
}

func TestSharedPoolBillingAtomicallyAccruesAndDeduplicates(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	cmd := sharedPoolTestCommand()
	proxyID := int64(9)
	cmd.UsesPlatformProxy, cmd.SharedPoolProxyID = true, &proxyID
	expectSharedPoolClaim(mock, cmd)
	mock.ExpectQuery(conditionalBalanceDeductSQL).WithArgs(5.0, int64(3)).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(95))
	expectSharedPoolRates(mock, cmd, 2000, 100)
	mock.ExpectExec("INSERT INTO shared_pool_earnings").WithArgs("shared-1", int64(2), int64(3), int64(6), int64(4), int64(5),
		5.0, 2000, 100, true, int64(9), "1.05000000", "3.95000000", "available").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	repo := NewUsageBillingRepository(nil, db)
	result, err := repo.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO usage_billing_dedup").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT request_fingerprint.*usage_billing_dedup").WillReturnRows(sqlmock.NewRows([]string{"fingerprint"}).AddRow(cmd.RequestFingerprint))
	mock.ExpectRollback()
	result, err = repo.Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.False(t, result.Applied)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolBillingOverdraftPendingAndDirectNoProxyFee(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	cmd := sharedPoolTestCommand()
	expectSharedPoolClaim(mock, cmd)
	mock.ExpectQuery(conditionalBalanceDeductSQL).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(overdraftBalanceDeductSQL).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(-2))
	expectSharedPoolRates(mock, cmd, 2000, 100)
	mock.ExpectExec("INSERT INTO shared_pool_earnings").WithArgs("shared-1", int64(2), int64(3), int64(6), int64(4), int64(5),
		5.0, 2000, 0, false, nil, "1.00000000", "4.00000000", "pending").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	result, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.True(t, result.BalanceOverdrafted)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolBillingRollsBackDeductionWhenLedgerFails(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	cmd := sharedPoolTestCommand()
	expectSharedPoolClaim(mock, cmd)
	mock.ExpectQuery(conditionalBalanceDeductSQL).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(95))
	expectSharedPoolRates(mock, cmd, 2000, 100)
	mock.ExpectExec("INSERT INTO shared_pool_earnings").WillReturnError(errors.New("ledger unavailable"))
	mock.ExpectRollback()
	_, err = NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.ErrorContains(t, err, "ledger unavailable")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolBillingSkipsNormalSelfAndSubscription(t *testing.T) {
	for _, change := range []func(*service.UsageBillingCommand){
		func(c *service.UsageBillingCommand) { c.SharedPoolOwnerID = 0 },
		func(c *service.UsageBillingCommand) { c.SharedPoolOwnerID = c.UserID },
		func(c *service.UsageBillingCommand) { c.SubscriptionID = &c.UserID },
		func(c *service.UsageBillingCommand) { c.BalanceCost = 0 },
	} {
		cmd := sharedPoolTestCommand()
		change(cmd)
		require.NoError(t, accrueSharedPoolEarnings(context.Background(), nil, cmd, nil))
	}
}

func TestSharedPoolBillingRejectsOwnerMismatchAndInvalidRates(t *testing.T) {
	for _, missingOwner := range []bool{true, false} {
		db, mock, err := sqlmock.New()
		require.NoError(t, err)
		cmd := sharedPoolTestCommand()
		expectSharedPoolClaim(mock, cmd)
		mock.ExpectQuery(conditionalBalanceDeductSQL).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(95))
		if missingOwner {
			mock.ExpectQuery("SELECT COALESCE\\(ur.platform_rate_bps").WillReturnError(sql.ErrNoRows)
		} else {
			expectSharedPoolRates(mock, cmd, 10001, 0)
		}
		mock.ExpectRollback()
		_, err = NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
		require.ErrorIs(t, err, service.ErrSharedPoolBillingInvalid)
		require.NoError(t, mock.ExpectationsWereMet())
		db.Close()
	}
}

func TestSharedPoolBillingRejectsMissingSharedGroupSnapshot(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	cmd := sharedPoolTestCommand()
	cmd.SharedPoolGroup = false
	expectSharedPoolClaim(mock, cmd)
	mock.ExpectQuery(conditionalBalanceDeductSQL).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(95))
	mock.ExpectRollback()
	_, err = NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.ErrorIs(t, err, service.ErrSharedPoolBillingInvalid)
	require.NoError(t, mock.ExpectationsWereMet())
}

func independentSharedPoolTestCommand(paid float64) *service.UsageBillingCommand {
	cmd := sharedPoolTestCommand()
	multiplier, platformBPS, proxyBPS, proxyID := 0.5, 500, 100, int64(9)
	cmd.SharedPoolGroup, cmd.BalanceCost = false, paid
	cmd.SharedPoolBaseCost, cmd.SharedPoolSettlementMultiplier = 10, &multiplier
	cmd.SharedPoolPlatformRateBPS, cmd.SharedPoolProxyRateBPS = &platformBPS, &proxyBPS
	cmd.UsesPlatformProxy, cmd.SharedPoolProxyID = true, &proxyID
	return cmd
}

func TestSharedPoolSettlementUsesSnapshotAndFixedOwnerAmount(t *testing.T) {
	for _, paid := range []float64{5, 10, 15} {
		t.Run(fmt.Sprintf("paid_%g", paid), func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			cmd := independentSharedPoolTestCommand(paid)
			expectSharedPoolClaim(mock, cmd)
			mock.ExpectQuery(conditionalBalanceDeductSQL).WithArgs(paid, int64(3)).
				WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(100 - paid))
			expectSharedPoolRates(mock, cmd, 7000, 2000)
			mock.ExpectExec("INSERT INTO shared_pool_earnings").WithArgs("shared-1", int64(2), int64(3), int64(6), int64(4), int64(5),
				paid, 500, 100, true, int64(9), fmt.Sprintf("%.8f", paid-4.7), "4.70000000", "available",
				"10.00000000", "0.5", "5.00000000", fmt.Sprintf("%.8f", paid-5)).WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()
			result, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
			require.NoError(t, err)
			require.True(t, result.Applied)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSharedPoolSettlementRejectsIncompleteSnapshotsAndUnderpayment(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*service.UsageBillingCommand)
	}{
		{"missing platform fee", func(c *service.UsageBillingCommand) { c.SharedPoolPlatformRateBPS = nil }},
		{"missing proxy fee", func(c *service.UsageBillingCommand) { c.SharedPoolProxyRateBPS = nil }},
		{"underpayment", func(c *service.UsageBillingCommand) { c.BalanceCost = 4.99999999 }},
		{"zero payment", func(c *service.UsageBillingCommand) { c.BalanceCost = 0 }},
		{"invalid snapshot fee", func(c *service.UsageBillingCommand) { *c.SharedPoolPlatformRateBPS = 10001 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			cmd := independentSharedPoolTestCommand(5)
			tc.change(cmd)
			expectSharedPoolClaim(mock, cmd)
			if cmd.BalanceCost > 0 {
				mock.ExpectQuery(conditionalBalanceDeductSQL).WithArgs(cmd.BalanceCost, int64(3)).
					WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow(100 - cmd.BalanceCost))
			}
			if cmd.BalanceCost >= 5 {
				expectSharedPoolRates(mock, cmd, 500, 100)
			}
			mock.ExpectRollback()
			result, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
			require.ErrorIs(t, err, service.ErrSharedPoolBillingInvalid)
			require.Nil(t, result)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
