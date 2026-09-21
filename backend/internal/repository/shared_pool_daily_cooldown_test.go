//go:build unit

package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

const sharedDailyCooldownUpdateSQL = `UPDATE accounts SET extra=COALESCE(extra,'{}'::jsonb)||jsonb_build_object($2::text,$3::jsonb) WHERE id=$1`

func newSharedDailyCooldownRepository(t *testing.T) (*sharedPoolRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, mock.ExpectationsWereMet())
		_ = db.Close()
	})
	return &sharedPoolRepository{db: db}, mock
}

func expectSharedDailyCooldownLock(mock sqlmock.Sqlmock) *sqlmock.ExpectedQuery {
	mock.ExpectBegin()
	return mock.ExpectQuery(regexp.QuoteMeta(`SELECT a.id FROM accounts a JOIN shared_pool_accounts s ON s.account_id=a.id
		WHERE a.id=$1 AND ($2=0 OR s.owner_user_id=$2) AND a.deleted_at IS NULL FOR UPDATE OF a,s`)).
		WithArgs(int64(41), int64(7))
}

func expectSharedDailyCooldownProfile(mock sqlmock.Sqlmock, in service.SharedPoolAccountUpdate) {
	mock.ExpectExec(`(?s)UPDATE accounts SET name=\$2,concurrency=\$3,.*extra=CASE WHEN NOT \$5 THEN COALESCE\(extra,'\{\}'::jsonb\).*WHERE id=\$1`).
		WithArgs(int64(41), in.Name, in.Concurrency, nil, in.ProxyChanged, in.ProxyID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`(?s)UPDATE accounts SET extra=jsonb_set\(extra,'\{anti_degrade,max_concurrency\}'.*WHERE id=\$1`).
		WithArgs(int64(41), in.Concurrency).WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectSharedDailyCooldownOutbox(mock sqlmock.Sqlmock) *sqlmock.ExpectedExec {
	return mock.ExpectExec("INSERT INTO scheduler_outbox").
		WithArgs(service.SchedulerOutboxEventAccountChanged, int64(41), nil, nil, sqlmock.AnyArg())
}

func TestSharedPoolDailyCooldownUpdateMergesOnlyDedicatedKey(t *testing.T) {
	for _, tc := range []struct {
		name      string
		cooldown  *service.SharedPoolDailyCooldown
		proxyID   *int64
		valueJSON string
	}{
		{name: "omitted preserves existing cooldown"},
		{name: "disabled discards stale fields", cooldown: &service.SharedPoolDailyCooldown{Start: "invalid", End: "invalid", Timezone: "Local"}, valueJSON: `{"enabled":false}`},
		{name: "enabled alongside random proxy", cooldown: &service.SharedPoolDailyCooldown{Enabled: true, Start: "23:30", End: "07:00", Timezone: "Asia/Tokyo"}, valueJSON: `{"enabled":true,"end":"07:00","start":"23:30","timezone":"Asia/Tokyo"}`},
		{name: "enabled alongside fixed proxy defaults timezone", cooldown: &service.SharedPoolDailyCooldown{Enabled: true, Start: "12:00", End: "13:00"}, proxyID: new(int64(9)), valueJSON: `{"enabled":true,"end":"13:00","start":"12:00","timezone":"Asia/Shanghai"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newSharedDailyCooldownRepository(t)
			in := service.SharedPoolAccountUpdate{Name: "updated", Concurrency: 7, ProxyChanged: tc.cooldown != nil, ProxyID: tc.proxyID, DailyCooldown: tc.cooldown}
			expectSharedDailyCooldownLock(mock).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(41))
			expectSharedDailyCooldownProfile(mock, in)
			if tc.valueJSON != "" {
				mock.ExpectExec(regexp.QuoteMeta(sharedDailyCooldownUpdateSQL)).
					WithArgs(int64(41), service.DailyCooldownExtraKey, tc.valueJSON).
					WillReturnResult(sqlmock.NewResult(0, 1))
			}
			expectSharedDailyCooldownOutbox(mock).WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()
			require.NoError(t, repo.UpdateSharedAccount(context.Background(), 7, 41, in))
		})
	}
}

func TestSharedPoolDailyCooldownUpdateRejectsInvalidBeforeTransaction(t *testing.T) {
	for _, tc := range []struct {
		name     string
		cooldown service.SharedPoolDailyCooldown
	}{
		{name: "same endpoints", cooldown: service.SharedPoolDailyCooldown{Enabled: true, Start: "12:00", End: "12:00"}},
		{name: "missing start", cooldown: service.SharedPoolDailyCooldown{Enabled: true, End: "12:00"}},
		{name: "invalid time", cooldown: service.SharedPoolDailyCooldown{Enabled: true, Start: "24:00", End: "12:00"}},
		{name: "invalid timezone", cooldown: service.SharedPoolDailyCooldown{Enabled: true, Start: "23:00", End: "07:00", Timezone: "Unknown/Invalid"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _ := newSharedDailyCooldownRepository(t)
			err := repo.UpdateSharedAccount(context.Background(), 7, 41, service.SharedPoolAccountUpdate{DailyCooldown: &tc.cooldown})
			require.Error(t, err)
			require.Contains(t, err.Error(), "每日冷却")
		})
	}
}

func TestSharedPoolDailyCooldownUpdateRequiresOwnerLock(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lockErr error
	}{
		{name: "missing or other owner account"},
		{name: "lock failure", lockErr: errors.New("lock failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newSharedDailyCooldownRepository(t)
			expected := expectSharedDailyCooldownLock(mock)
			wantErr := tc.lockErr
			if tc.lockErr == nil {
				expected.WillReturnRows(sqlmock.NewRows([]string{"id"}))
				wantErr = service.ErrSharedPoolAccountNotFound
			} else {
				expected.WillReturnError(tc.lockErr)
			}
			mock.ExpectRollback()
			err := repo.UpdateSharedAccount(context.Background(), 7, 41, service.SharedPoolAccountUpdate{DailyCooldown: &service.SharedPoolDailyCooldown{}})
			require.ErrorIs(t, err, wantErr)
		})
	}
}

func TestSharedPoolDailyCooldownUpdateRollsBackOnWriteFailure(t *testing.T) {
	for _, stage := range []string{"daily cooldown", "outbox"} {
		t.Run(stage, func(t *testing.T) {
			repo, mock := newSharedDailyCooldownRepository(t)
			in := service.SharedPoolAccountUpdate{Name: "updated", Concurrency: 7, DailyCooldown: &service.SharedPoolDailyCooldown{}}
			expectSharedDailyCooldownLock(mock).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(41))
			expectSharedDailyCooldownProfile(mock, in)
			write := mock.ExpectExec(regexp.QuoteMeta(sharedDailyCooldownUpdateSQL)).WithArgs(int64(41), service.DailyCooldownExtraKey, `{"enabled":false}`)
			writeErr := errors.New(stage + " write failed")
			if stage == "daily cooldown" {
				write.WillReturnError(writeErr)
			} else {
				write.WillReturnResult(sqlmock.NewResult(0, 1))
				expectSharedDailyCooldownOutbox(mock).WillReturnError(writeErr)
			}
			mock.ExpectRollback()
			require.ErrorIs(t, repo.UpdateSharedAccount(context.Background(), 7, 41, in), writeErr)
		})
	}
}
