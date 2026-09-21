//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func sharedSubscriptionTestCommand() *service.UsageBillingCommand {
	cmd := independentSharedPoolTestCommand(0)
	cmd.SubscriptionID, cmd.SubscriptionCost = new(int64(17)), 10
	return cmd
}

func TestSharedPoolSubscriptionBillingUsesQuotaAndIndependentSnapshot(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
	cmd := sharedSubscriptionTestCommand()
	expectSharedPoolClaim(mock, cmd)
	mock.ExpectExec("UPDATE user_subscriptions us").WithArgs(10.0, int64(17)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectSharedPoolRates(mock, cmd, 7000, 2000)
	mock.ExpectExec("INSERT INTO shared_pool_earnings").WithArgs("shared-1", int64(2), int64(3), int64(6), int64(4), int64(5),
		10.0, 500, 100, true, int64(9), "5.30000000", "4.70000000", "available",
		"10.00000000", "0.5", "5.00000000", "5.00000000").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	result, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.True(t, result.Applied)
	require.Nil(t, result.NewBalance)
	require.False(t, result.BalanceOverdrafted)
	// 不配置任何余额扣减 SQL 预期，确保订阅请求只消耗订阅额度。
	mock.ExpectBegin()
	mock.ExpectQuery("INSERT INTO usage_billing_dedup").WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery("SELECT request_fingerprint.*usage_billing_dedup").WillReturnRows(sqlmock.NewRows([]string{"fingerprint"}).AddRow(cmd.RequestFingerprint))
	mock.ExpectRollback()
	result, err = NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.NoError(t, err)
	require.False(t, result.Applied)
}

func TestSharedPoolSubscriptionBillingRejectsInvalidSources(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*service.UsageBillingCommand)
	}{
		{"mixed balance and subscription", func(c *service.UsageBillingCommand) { c.BalanceCost = 5 }},
		{"missing subscription ID", func(c *service.UsageBillingCommand) { c.SubscriptionID = nil }},
		{"zero subscription ID", func(c *service.UsageBillingCommand) { c.SubscriptionID = new(int64(0)) }},
		{"negative subscription ID", func(c *service.UsageBillingCommand) { c.SubscriptionID = new(int64(-1)) }},
		{"negative subscription cost", func(c *service.UsageBillingCommand) { c.SubscriptionCost = -1 }},
		{"nonfinite subscription cost", func(c *service.UsageBillingCommand) { c.SubscriptionCost = math.Inf(1) }},
		{"underfunded subscription", func(c *service.UsageBillingCommand) { c.SubscriptionCost = 4.99999999 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := sharedSubscriptionTestCommand()
			tc.change(cmd)
			require.ErrorIs(t, accrueSharedPoolEarnings(context.Background(), nil, cmd, nil), service.ErrSharedPoolBillingInvalid)
		})
	}
}

func TestSharedPoolSubscriptionBillingSkipsSelfAndLegacy(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		cmd := sharedSubscriptionTestCommand()
		if legacy {
			cmd.SharedPoolSettlementMultiplier = nil
		} else {
			cmd.SharedPoolOwnerID = cmd.UserID
		}
		require.NoError(t, accrueSharedPoolEarnings(context.Background(), nil, cmd, nil))
	}
}

func TestSharedPoolSubscriptionBillingLedgerFailureRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
	cmd := sharedSubscriptionTestCommand()
	expectSharedPoolClaim(mock, cmd)
	mock.ExpectExec("UPDATE user_subscriptions us").WithArgs(10.0, int64(17)).WillReturnResult(sqlmock.NewResult(0, 1))
	expectSharedPoolRates(mock, cmd, 500, 100)
	failure := errors.New("subscription ledger insert failed")
	mock.ExpectExec("INSERT INTO shared_pool_earnings").WillReturnError(failure)
	mock.ExpectRollback()
	result, err := NewUsageBillingRepository(nil, db).Apply(context.Background(), cmd)
	require.ErrorIs(t, err, failure)
	require.Nil(t, result)
}
