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

const sharedPoolTransferRedeemSQL = `(?s)INSERT INTO redeem_codes.*SELECT 'SHARED-' \|\| id::text, 'shared_pool_transfer', amount, 'used',.*user_id, created_at, created_at, '共享收益转入余额'.*FROM shared_pool_earnings_transfers WHERE id = \$1 AND amount > 0`

func expectSharedPoolTransferCredit(mock sqlmock.Sqlmock) {
	expectSharedPoolTransferClaim(mock, "3.95000000")
	mock.ExpectQuery(`UPDATE users SET balance = balance \+ \$1::numeric, updated_at = NOW\(\)\s+WHERE id = \$2 AND deleted_at IS NULL RETURNING balance::text`).
		WithArgs("3.95000000", int64(6)).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow("13.95000000"))
	mock.ExpectExec("UPDATE shared_pool_earnings_transfers SET amount").WithArgs("3.95000000", "13.95000000", int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestSharedPoolTransferRedeemFailureRollsBackBalanceAndClaim(t *testing.T) {
	for _, failure := range []string{"insert failed", "unique source conflict", "missing source", "affected rows failed"} {
		t.Run(failure, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			expectSharedPoolTransferCredit(mock)
			insert := mock.ExpectExec(sharedPoolTransferRedeemSQL).WithArgs(int64(7))
			switch failure {
			case "missing source":
				insert.WillReturnResult(sqlmock.NewResult(0, 0))
			case "affected rows failed":
				insert.WillReturnResult(sqlmock.NewErrorResult(errors.New(failure)))
			default:
				insert.WillReturnError(errors.New(failure))
			}
			mock.ExpectRollback()
			result, err := NewSharedPoolEarningsRepository(db).Transfer(context.Background(), 6)
			require.Error(t, err)
			require.Nil(t, result)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSharedPoolTransferRedeemRepeatWithoutNewEarningsWritesNoRecord(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	expectSharedPoolTransferCredit(mock)
	mock.ExpectExec(sharedPoolTransferRedeemSQL).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	expectSharedPoolTransferClaim(mock, "0.00000000")
	mock.ExpectRollback()
	repo := NewSharedPoolEarningsRepository(db)
	_, err = repo.Transfer(context.Background(), 6)
	require.NoError(t, err)
	result, err := repo.Transfer(context.Background(), 6)
	require.ErrorIs(t, err, service.ErrSharedPoolEarningsEmpty)
	require.Nil(t, result)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolTransferRedeemCommitFailureDoesNotReturnSuccess(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	expectSharedPoolTransferCredit(mock)
	mock.ExpectExec(sharedPoolTransferRedeemSQL).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit().WillReturnError(errors.New("commit failed"))
	result, err := NewSharedPoolEarningsRepository(db).Transfer(context.Background(), 6)
	require.ErrorContains(t, err, "commit failed")
	require.Nil(t, result)
	require.NoError(t, mock.ExpectationsWereMet())
}
