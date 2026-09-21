//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func expectSharedPoolTransferClaim(mock sqlmock.Sqlmock, amount string) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT balance::text, status FROM users.*FOR UPDATE").WithArgs(int64(6)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "status"}).AddRow("10.00000000", service.StatusActive))
	mock.ExpectExec("(?s)UPDATE shared_pool_earnings e.*consumer_balance_restored.*debtor.balance >= 0").
		WithArgs(int64(6)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery("INSERT INTO shared_pool_earnings_transfers").WithArgs(int64(6), "10.00000000").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectQuery("(?s)WITH claimed AS.*status = 'available'.*FOR UPDATE.*UPDATE shared_pool_earnings.*SELECT COALESCE").
		WithArgs(int64(6), int64(7)).WillReturnRows(sqlmock.NewRows([]string{"amount"}).AddRow(amount))
}

func TestSharedPoolEarningsTransferCreditsOnlyBalance(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	expectSharedPoolTransferClaim(mock, "3.95000000")
	mock.ExpectQuery(`UPDATE users SET balance = balance \+ \$1::numeric, updated_at = NOW\(\)\s+WHERE id = \$2 AND deleted_at IS NULL RETURNING balance::text`).
		WithArgs("3.95000000", int64(6)).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow("13.95000000"))
	mock.ExpectExec("UPDATE shared_pool_earnings_transfers SET amount").WithArgs("3.95000000", "13.95000000", int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(sharedPoolTransferRedeemSQL).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	result, err := NewSharedPoolEarningsRepository(db).Transfer(context.Background(), 6)
	require.NoError(t, err)
	require.Equal(t, int64(7), result.ID)
	require.Equal(t, 3.95, result.Amount)
	require.Equal(t, 13.95, result.Balance)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolEarningsTransferEmptyRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	expectSharedPoolTransferClaim(mock, "0")
	mock.ExpectRollback()
	_, err = NewSharedPoolEarningsRepository(db).Transfer(context.Background(), 6)
	require.ErrorIs(t, err, service.ErrSharedPoolEarningsEmpty)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolEarningsTransferFailureRollsBackClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	expectSharedPoolTransferClaim(mock, "3.95000000")
	mock.ExpectQuery("UPDATE users SET balance").WillReturnError(errors.New("balance write failed"))
	mock.ExpectRollback()
	_, err = NewSharedPoolEarningsRepository(db).Transfer(context.Background(), 6)
	require.ErrorContains(t, err, "balance write failed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolEarningsListScopesOwnerAndPagination(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("(?s)SELECT COUNT.*owner_user_id = \\$1.*account_id = \\$2.*effective_status = \\$3").
		WithArgs(int64(6), int64(4), "pending").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery("(?s)SELECT id, account_id.*LIMIT \\$4 OFFSET \\$5").
		WithArgs(int64(6), int64(4), "pending", 100, int64(100)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "account_id", "owner_user_id", "group_id", "billing_amount", "platform_rate_bps", "proxy_rate_bps", "uses_platform_proxy", "platform_amount", "owner_amount", "status", "transfer_id", "created_at"}))
	result, err := NewSharedPoolEarningsRepository(db).List(context.Background(), 6, service.SharedPoolEarningsFilter{Page: 2, PageSize: 500, AccountID: 4, Status: "pending"})
	require.NoError(t, err)
	require.Equal(t, 100, result.PageSize)
	require.Empty(t, result.Items)
	require.NoError(t, mock.ExpectationsWereMet())
}
