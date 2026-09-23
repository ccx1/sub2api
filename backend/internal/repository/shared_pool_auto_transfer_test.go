//go:build unit

package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolAutoTransferGetDefaults(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT enabled, threshold, daily_time.*FROM shared_pool_auto_transfer_settings WHERE user_id").
		WithArgs(int64(6)).WillReturnError(sql.ErrNoRows)
	result, err := NewSharedPoolAutoTransferRepository(db).Get(context.Background(), 6)
	require.NoError(t, err)
	require.Equal(t, &service.SharedPoolAutoTransferSettings{Threshold: 1, DailyTime: "00:00", Timezone: timezone.Name()}, result)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolAutoTransferSavePreservesDailyMarker(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery(`(?s)INSERT INTO shared_pool_auto_transfer_settings.*ON CONFLICT \(user_id\) DO UPDATE SET enabled = EXCLUDED.enabled, threshold = EXCLUDED.threshold, daily_time = EXCLUDED.daily_time, updated_at = NOW\(\) RETURNING`).
		WithArgs(int64(6), true, "0.00000001", "09:30").
		WillReturnRows(sqlmock.NewRows([]string{"enabled", "threshold", "daily_time", "last_run_date"}).
			AddRow(true, "0.00000001", "09:30", "2026-09-23"))
	result, err := NewSharedPoolAutoTransferRepository(db).Save(context.Background(), 6,
		service.SharedPoolAutoTransferUpdate{Enabled: true, Threshold: 0.00000001, DailyTime: "09:30"})
	require.NoError(t, err)
	require.Equal(t, "2026-09-23", result.LastRunDate)
	require.Equal(t, timezone.Name(), result.Timezone)
	require.Equal(t, 0.00000001, result.Threshold)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolAutoTransferSaveInvalidSettingsNeverWrites(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := NewSharedPoolAutoTransferRepository(db)
	for _, input := range []service.SharedPoolAutoTransferUpdate{
		{Threshold: 0, DailyTime: "09:30"},
		{Threshold: 1.000000001, DailyTime: "09:30"},
		{Threshold: 1000000001, DailyTime: "09:30"},
		{Threshold: 1, DailyTime: "24:00"},
		{Threshold: 1, DailyTime: "9:30"},
	} {
		_, err := repo.Save(context.Background(), 6, input)
		require.ErrorIs(t, err, service.ErrSharedPoolAutoTransferInvalid)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolAutoTransferDueUsersUseServerDateAndKeyset(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	now := time.Date(2026, 9, 23, 0, 15, 0, 0, timezone.Location()).UTC()
	mock.ExpectQuery(`(?s)SELECT s.user_id.*u.deleted_at IS NULL AND u.status = 'active'.*s.enabled AND s.user_id > \$1.*s.last_run_date < \$2::date.*s.daily_time <= \$3 ORDER BY s.user_id LIMIT \$4`).
		WithArgs(int64(5), "2026-09-23", "00:15", 20).
		WillReturnRows(sqlmock.NewRows([]string{"user_id"}).AddRow(6).AddRow(8))
	ids, err := NewSharedPoolAutoTransferRepository(db).DueUserIDs(context.Background(), now, 5, 20)
	require.NoError(t, err)
	require.Equal(t, []int64{6, 8}, ids)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolAutoTransferDueUsersPropagatesRowFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("SELECT s.user_id").WillReturnRows(sqlmock.NewRows([]string{"user_id"}).
		AddRow(6).RowError(0, errors.New("read failed")))
	_, err = NewSharedPoolAutoTransferRepository(db).DueUserIDs(context.Background(), time.Now(), 0, 20)
	require.ErrorContains(t, err, "read failed")
	require.NoError(t, mock.ExpectationsWereMet())
}
