//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

var sharedPoolAutoTransferNow = time.Date(2026, 9, 23, 10, 0, 0, 0, timezone.Location())

func expectSharedPoolAutoTransferStart(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT status FROM users.*FOR UPDATE").WithArgs(int64(6)).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow(service.StatusActive))
}

func expectSharedPoolAutoTransferAvailable(mock sqlmock.Sqlmock, threshold, available string) {
	expectSharedPoolAutoTransferStart(mock)
	mock.ExpectQuery("SELECT enabled, threshold::text, daily_time.*FOR UPDATE").WithArgs(int64(6)).
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "threshold", "daily_time", "last_run_date"}).
			AddRow(true, threshold, "09:30", "2026-09-22"))
	expectSharedPoolAutoTransferRelease(mock)
	mock.ExpectQuery("SELECT COALESCE.*FROM shared_pool_earnings.*status = 'available' AND owner_amount > 0").
		WithArgs(int64(6)).WillReturnRows(sqlmock.NewRows([]string{"amount"}).AddRow(available))
}

func expectSharedPoolAutoTransferRelease(mock sqlmock.Sqlmock) {
	mock.ExpectExec("UPDATE shared_pool_earnings e.*consumer_balance_restored.*debtor.balance >= 0").
		WithArgs(int64(6)).WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectSharedPoolAutoTransferCredit(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT balance::text, status FROM users.*FOR UPDATE").WithArgs(int64(6)).
		WillReturnRows(sqlmock.NewRows([]string{"balance", "status"}).AddRow("10.00000000", service.StatusActive))
	expectSharedPoolAutoTransferRelease(mock)
	mock.ExpectQuery("INSERT INTO shared_pool_earnings_transfers").WithArgs(int64(6), "10.00000000").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(7))
	mock.ExpectQuery("WITH claimed AS.*status = 'available'.*FOR UPDATE.*SELECT COALESCE").
		WithArgs(int64(6), int64(7)).WillReturnRows(sqlmock.NewRows([]string{"amount"}).AddRow("3.95000000"))
	mock.ExpectQuery(`UPDATE users SET balance = balance \+ \$1::numeric`).
		WithArgs("3.95000000", int64(6)).WillReturnRows(sqlmock.NewRows([]string{"balance"}).AddRow("13.95000000"))
	mock.ExpectExec("UPDATE shared_pool_earnings_transfers SET amount").WithArgs("3.95000000", "13.95000000", int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestSharedPoolAutoTransferRunAtThresholdCreditsAndMarksToday(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	expectSharedPoolAutoTransferAvailable(mock, "3.95000000", "3.95000000")
	expectSharedPoolAutoTransferCredit(mock)
	mock.ExpectExec(sharedPoolTransferRedeemSQL).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE shared_pool_auto_transfer_settings SET last_run_date").
		WithArgs(int64(6), "2026-09-23").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	result, err := NewSharedPoolAutoTransferRepository(db).RunDue(context.Background(), 6, sharedPoolAutoTransferNow)
	require.NoError(t, err)
	require.Equal(t, &service.SharedPoolEarningsTransfer{ID: 7, Amount: 3.95, Balance: 13.95}, result)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolAutoTransferRunBelowThresholdMarksTodayWithoutTransfer(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	// 大额相差八位小数时 float64 已无法可靠区分，必须以十进制比较。
	expectSharedPoolAutoTransferAvailable(mock, "1000000000.00000000", "999999999.99999999")
	mock.ExpectExec("UPDATE shared_pool_auto_transfer_settings SET last_run_date").
		WithArgs(int64(6), "2026-09-23").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	result, err := NewSharedPoolAutoTransferRepository(db).RunDue(context.Background(), 6, sharedPoolAutoTransferNow)
	require.NoError(t, err)
	require.Nil(t, result)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolAutoTransferRunRechecksEligibility(t *testing.T) {
	for _, tc := range []struct {
		name, clock, last string
		enabled           bool
	}{
		{name: "disabled", clock: "09:30", enabled: false},
		{name: "already ran", clock: "09:30", last: "2026-09-23", enabled: true},
		{name: "future marker", clock: "09:30", last: "2026-09-24", enabled: true},
		{name: "not yet due", clock: "10:01", enabled: true},
		{name: "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			expectSharedPoolAutoTransferStart(mock)
			rows := sqlmock.NewRows([]string{"enabled", "threshold", "daily_time", "last_run_date"})
			if tc.name != "missing" {
				rows.AddRow(tc.enabled, "1.00000000", tc.clock, tc.last)
			}
			mock.ExpectQuery("SELECT enabled, threshold::text, daily_time.*FOR UPDATE").WithArgs(int64(6)).WillReturnRows(rows)
			mock.ExpectCommit()
			result, err := NewSharedPoolAutoTransferRepository(db).RunDue(context.Background(), 6, sharedPoolAutoTransferNow)
			require.NoError(t, err)
			require.Nil(t, result)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSharedPoolAutoTransferRunFailureRollsBackWithoutSuccess(t *testing.T) {
	for _, failure := range []string{"history", "marker", "commit"} {
		t.Run(failure, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			expectSharedPoolAutoTransferAvailable(mock, "1.00000000", "3.95000000")
			expectSharedPoolAutoTransferCredit(mock)
			write := mock.ExpectExec(sharedPoolTransferRedeemSQL).WithArgs(int64(7))
			if failure == "history" {
				write.WillReturnError(errors.New("history failed"))
			} else {
				write.WillReturnResult(sqlmock.NewResult(1, 1))
				marker := mock.ExpectExec("UPDATE shared_pool_auto_transfer_settings SET last_run_date").WithArgs(int64(6), "2026-09-23")
				if failure == "marker" {
					marker.WillReturnError(errors.New("marker failed"))
				} else {
					marker.WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			if failure == "commit" {
				mock.ExpectCommit().WillReturnError(errors.New("commit failed"))
			} else {
				mock.ExpectRollback()
			}
			result, err := NewSharedPoolAutoTransferRepository(db).RunDue(context.Background(), 6, sharedPoolAutoTransferNow)
			require.ErrorContains(t, err, failure+" failed")
			require.Nil(t, result)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
