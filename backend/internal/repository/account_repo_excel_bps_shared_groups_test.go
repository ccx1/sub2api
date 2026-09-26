package repository

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type bps403SharedGroupCase struct {
	name, platform, kind, status               string
	owned, consent, shared, exclusive, allowed bool
}

func TestMoveExcelBPSOn403SharedGroupPolicyUsesLockedState(t *testing.T) {
	for _, tc := range []bps403SharedGroupCase{
		{"legacy_shared_standard", "openai", "standard", "active", true, false, true, false, true},
		{"legacy_ordinary", "openai", "standard", "active", true, false, false, false, false},
		{"legacy_exclusive", "openai", "standard", "active", true, false, true, true, false},
		{"legacy_subscription", "openai", "subscription", "active", true, false, true, false, false},
		{"consented_ordinary", "openai", "standard", "active", true, true, false, false, true},
		{"consented_subscription", "openai", "subscription", "active", true, true, false, true, true},
		{"consented_composite", "composite", "standard", "active", true, true, false, false, false},
		{"consented_inactive", "openai", "standard", "disabled", true, true, true, false, false},
		{"ordinary_composite", "composite", "standard", "active", false, false, false, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) { testBPS403SharedGroupPolicy(t, tc) })
	}
}

func testBPS403SharedGroupPolicy(t *testing.T, tc bps403SharedGroupCase) {
	t.Helper()
	repo, mock, account := newBPS403SharedGroupFixture(t)
	account.Extra[service.SharedPoolOwnerKey] = int64(42)
	account.Extra[service.SharedPoolDispatchConsentKey] = !tc.consent
	extra := map[string]any{}
	if tc.owned {
		extra[service.SharedPoolOwnerKey] = int64(42)
		extra[service.SharedPoolDispatchConsentKey] = tc.consent
	}
	raw, err := json.Marshal(extra)
	require.NoError(t, err)
	mock.ExpectBegin()
	expectBPS403LockedExtra(mock, 7, string(raw))
	expectBPS403TargetGroup(mock, tc)
	if tc.allowed {
		expectBPS403Memberships(mock, 3)
		expectBPS403GroupMove(mock, 7, tc.owned)
		mock.ExpectCommit()
	} else {
		mock.ExpectRollback()
	}
	changed, err := repo.MoveExcelBPSOn403(context.Background(), account)
	if tc.allowed {
		require.NoError(t, err)
		require.True(t, changed)
	} else {
		require.ErrorContains(t, err, "分组与账号不兼容")
		require.False(t, changed)
	}
	require.Equal(t, []int64{3}, account.GroupIDs)
	require.Equal(t, !tc.consent, account.Extra[service.SharedPoolDispatchConsentKey])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMoveExcelBPSOn403SharedNoopDoesNotChangeAssignment(t *testing.T) {
	for _, current := range []int64{7, 4} {
		t.Run(strconv.FormatInt(current, 10), func(t *testing.T) {
			repo, mock, account := newBPS403SharedGroupFixture(t)
			if current == 7 {
				account.GroupIDs = []int64{7}
			}
			mock.ExpectBegin()
			expectBPS403LockedExtra(mock, 7, `{"shared_pool_owner_id":42}`)
			expectBPS403TargetGroup(mock, bps403SharedGroupCase{platform: "openai", kind: "standard", status: "active", shared: true})
			expectBPS403Memberships(mock, current)
			mock.ExpectCommit()
			changed, err := repo.MoveExcelBPSOn403(context.Background(), account)
			require.NoError(t, err)
			require.False(t, changed)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMoveExcelBPSOn403SharedLeaveAllPreservesMetadata(t *testing.T) {
	repo, mock, account := newBPS403SharedGroupFixture(t)
	account.Extra[service.ExcelBPS403TargetGroupIDKey] = 0
	mock.ExpectBegin()
	expectBPS403LockedExtra(mock, 0, `{"shared_pool_owner_id":42,"shared_pool_enabled":false}`)
	expectBPS403Memberships(mock, 3)
	expectBPS403GroupMove(mock, 0, true)
	mock.ExpectCommit()
	changed, err := repo.MoveExcelBPSOn403(context.Background(), account)
	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMoveExcelBPSOn403SharedAssignmentFailureRollsBack(t *testing.T) {
	repo, mock, account := newBPS403SharedGroupFixture(t)
	failure := errors.New("assignment unavailable")
	mock.ExpectBegin()
	expectBPS403LockedExtra(mock, 7, `{"shared_pool_owner_id":42}`)
	expectBPS403TargetGroup(mock, bps403SharedGroupCase{platform: "openai", kind: "standard", status: "active", shared: true})
	expectBPS403Memberships(mock, 3)
	mock.ExpectExec("UPDATE shared_pool_accounts SET assigned=TRUE").WithArgs(int64(27)).WillReturnError(failure)
	mock.ExpectRollback()
	changed, err := repo.MoveExcelBPSOn403(context.Background(), account)
	require.ErrorIs(t, err, failure)
	require.False(t, changed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMoveExcelBPSOn403StaleAccountSkipsTargetLock(t *testing.T) {
	repo, mock, account := newBPS403SharedGroupFixture(t)
	mock.ExpectBegin()
	mock.ExpectQuery("(?s)SELECT extra FROM accounts.*FOR UPDATE").
		WithArgs(int64(27), `{"access_token":"test-token"}`, "7").
		WillReturnRows(sqlmock.NewRows([]string{"extra"}))
	mock.ExpectCommit()
	changed, err := repo.MoveExcelBPSOn403(context.Background(), account)
	require.NoError(t, err)
	require.False(t, changed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func newBPS403SharedGroupFixture(t *testing.T) (*accountRepository, sqlmock.Sqlmock, *service.Account) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	account := &service.Account{ID: 27, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "test-token"}, GroupIDs: []int64{3},
		Extra: map[string]any{"openai_excel_bps": true, service.ExcelBPSAutoMoveOn403Key: true, service.ExcelBPS403TargetGroupIDKey: 7}}
	return newAccountRepositoryWithSQL(client, db, nil), mock, account
}

func expectBPS403LockedExtra(mock sqlmock.Sqlmock, target int64, extra string) {
	mock.ExpectQuery("(?s)SELECT extra FROM accounts.*FOR UPDATE").
		WithArgs(int64(27), `{"access_token":"test-token"}`, strconv.FormatInt(target, 10)).
		WillReturnRows(sqlmock.NewRows([]string{"extra"}).AddRow(extra))
}

func expectBPS403TargetGroup(mock sqlmock.Sqlmock, tc bps403SharedGroupCase) {
	mock.ExpectQuery("(?s)SELECT platform, status, subscription_type, is_exclusive, is_shared_pool, require_oauth_only FROM groups.*FOR SHARE").
		WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"platform", "status", "subscription_type", "is_exclusive", "is_shared_pool", "require_oauth_only"}).
		AddRow(tc.platform, tc.status, tc.kind, tc.exclusive, tc.shared, false))
}

func expectBPS403Memberships(mock sqlmock.Sqlmock, current int64) {
	mock.ExpectQuery("SELECT group_id FROM account_groups.*FOR UPDATE").WithArgs(int64(27)).
		WillReturnRows(sqlmock.NewRows([]string{"group_id"}).AddRow(current))
}

func expectBPS403GroupMove(mock sqlmock.Sqlmock, target int64, shared bool) {
	if shared {
		mock.ExpectExec("UPDATE shared_pool_accounts SET assigned=TRUE").WithArgs(int64(27)).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectExec("DELETE FROM account_groups").WithArgs(int64(27), target).WillReturnResult(sqlmock.NewResult(0, 1))
	if target > 0 {
		mock.ExpectExec("INSERT INTO account_groups").WithArgs(int64(27), target).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectExec("UPDATE accounts SET updated_at").WithArgs(int64(27)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO scheduler_outbox").WillReturnResult(sqlmock.NewResult(0, 1))
}
