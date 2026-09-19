package repository

import (
	"context"
	"database/sql/driver"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func newSpendGuardRepoMock(t *testing.T) (*apiKeyRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return &apiKeyRepository{sql: db}, mock
}

func expectSpendGuardLockedKey(mock sqlmock.Sqlmock, status string) {
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT key, name, status FROM api_keys.*FOR UPDATE").WithArgs(int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"key", "name", "status"}).AddRow("test-key", "name", status))
}

func expectSpendGuardFreezeState(mock sqlmock.Sqlmock, exists bool, released driver.Value) {
	rows := sqlmock.NewRows([]string{"released_at"})
	if exists {
		rows.AddRow(released)
	}
	mock.ExpectQuery("SELECT released_at FROM api_key_spend_guard_freezes").WithArgs(int64(7)).WillReturnRows(rows)
}

func TestSpendGuardFreezeRollsBackStateWhenEventFails(t *testing.T) {
	repo, mock := newSpendGuardRepoMock(t)
	expectSpendGuardLockedKey(mock, service.StatusAPIKeyActive)
	expectSpendGuardFreezeState(mock, false, nil)
	mock.ExpectExec("INSERT INTO api_key_spend_guard_freezes").WithArgs(int64(7), "error rate").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE api_keys SET status = 'disabled'").WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	failure := errors.New("event write failed")
	mock.ExpectExec("INSERT INTO spend_guard_events").WithArgs(int64(7), "name", "frozen", "error rate").WillReturnError(failure)
	mock.ExpectRollback()
	_, changed, err := repo.FreezeAPIKeyForSpendGuard(context.Background(), service.SpendGuardFreezeInput{APIKeyID: 7, Reason: "error rate"})
	require.ErrorIs(t, err, failure)
	require.False(t, changed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSpendGuardFreezeCommitIsAtomic(t *testing.T) {
	repo, mock := newSpendGuardRepoMock(t)
	expectSpendGuardLockedKey(mock, service.StatusAPIKeyActive)
	expectSpendGuardFreezeState(mock, false, nil)
	mock.ExpectExec("INSERT INTO api_key_spend_guard_freezes").WithArgs(int64(7), "token velocity").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE api_keys SET status = 'disabled'").WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO spend_guard_events").WithArgs(int64(7), "name", "frozen", "token velocity").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	key, changed, err := repo.FreezeAPIKeyForSpendGuard(context.Background(), service.SpendGuardFreezeInput{APIKeyID: 7, Reason: "token velocity"})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "test-key", key)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSpendGuardStaleSweepCannotRefreezeAfterManualRelease(t *testing.T) {
	for _, tc := range []struct {
		name   string
		frozen bool
		status string
	}{{"released after statistics", false, service.StatusAPIKeyActive}, {"another worker already froze", true, service.StatusAPIKeyDisabled}} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newSpendGuardRepoMock(t)
			expectSpendGuardLockedKey(mock, tc.status)
			var released driver.Value = time.Now()
			if tc.frozen {
				released = nil
			}
			expectSpendGuardFreezeState(mock, true, released)
			mock.ExpectRollback()
			_, changed, err := repo.FreezeAPIKeyForSpendGuard(context.Background(), service.SpendGuardFreezeInput{APIKeyID: 7, Reason: "error rate"})
			require.NoError(t, err)
			require.False(t, changed)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestSpendGuardUnfreezeReleasesBeforeReactivation(t *testing.T) {
	repo, mock := newSpendGuardRepoMock(t)
	expectSpendGuardLockedKey(mock, service.StatusAPIKeyDisabled)
	expectSpendGuardFreezeState(mock, true, nil)
	mock.ExpectExec("UPDATE api_key_spend_guard_freezes SET released_at").WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE api_keys SET status = 'active'").WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO spend_guard_events").WithArgs(int64(7), "name", "unfrozen", "manual").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	key, changed, err := repo.UnfreezeAPIKeyForSpendGuard(context.Background(), 7)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, "test-key", key)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSpendGuardListIncludesDormantFrozenKeysAndEvents(t *testing.T) {
	repo, mock := newSpendGuardRepoMock(t)
	mock.ExpectQuery(regexp.QuoteMeta(spendGuardOffendersSQL)).WithArgs(30).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "user_id", "status", "reqs", "toks", "errs", "frozen", "released_at"}).
			AddRow(7, "frozen", 2, service.StatusAPIKeyDisabled, 0, 0, 0, true, nil).
			AddRow(8, "active", 2, service.StatusAPIKeyActive, 8, 3000, 2, false, nil))
	items, err := repo.ListSpendGuardOffenders(context.Background(), 30)
	require.NoError(t, err)
	require.Len(t, items, 2)
	require.True(t, items[0].Frozen)
	require.Zero(t, items[0].Requests)
	require.Equal(t, float64(100), items[1].TokensPerMin)
	require.InDelta(t, 0.2, items[1].ErrRate, 1e-9)

	mock.ExpectQuery("SELECT created_at, api_key_id, api_key_name, action, reason").WithArgs(100).
		WillReturnRows(sqlmock.NewRows([]string{"created_at", "api_key_id", "api_key_name", "action", "reason"}).AddRow(time.Now(), 7, "frozen", "frozen", "error rate"))
	events, err := (&apiKeyRepository{sql: repo.sql}).ListSpendGuardEvents(context.Background(), 100)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, int64(7), events[0].APIKeyID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSpendGuardTriggerErrorMapsToForbidden(t *testing.T) {
	err := translateSpendGuardUpdateError(&pq.Error{Code: "23514", Constraint: "api_key_spend_guard_frozen"})
	require.ErrorIs(t, err, service.ErrAPIKeySpendGuardFrozen)
	other := &pq.Error{Code: "23514", Constraint: "other_constraint"}
	require.Same(t, other, translateSpendGuardUpdateError(other))
}
