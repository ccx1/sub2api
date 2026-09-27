//go:build unit

package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const sharedReimportLookup = `SELECT s.account_id,a.deleted_at FROM shared_pool_accounts s JOIN accounts a ON a.id=s.account_id WHERE s.credential_fingerprint=$1 FOR UPDATE OF a,s`

func expectSharedCredentialAbsent(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta(sharedReimportLookup)).WithArgs(sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "deleted_at"})).RowsWillBeClosed()
}

func expectSharedReimportDeleted(mock sqlmock.Sqlmock) {
	mock.ExpectQuery(regexp.QuoteMeta(sharedReimportLookup)).WithArgs("fixture-fingerprint").
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "deleted_at"}).
			AddRow(17, time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC))).RowsWillBeClosed()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE shared_pool_accounts SET credential_fingerprint=$2,updated_at=NOW() WHERE account_id=$1`)).
		WithArgs(int64(17), "deleted:17").WillReturnResult(sqlmock.NewResult(0, 1))
}

func expectSharedReimportWrites(mock sqlmock.Sqlmock, ownerID int64, failure string) error {
	failureErr := errors.New("fixture " + failure + " failure")
	account := mock.ExpectQuery(`INSERT INTO "accounts"`)
	if failure == "account" {
		account.WillReturnError(failureErr)
		return failureErr
	}
	account.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(41))
	owner := mock.ExpectExec("INSERT INTO shared_pool_accounts").
		WithArgs(int64(41), ownerID, "fixture-fingerprint", true, true)
	if failure == "ownership" {
		owner.WillReturnError(failureErr)
		return failureErr
	}
	owner.WillReturnResult(sqlmock.NewResult(1, 1))
	link := mock.ExpectExec(`INSERT INTO "account_groups"`).
		WithArgs(50, sqlmock.AnyArg(), int64(41), int64(11))
	if failure == "association" {
		link.WillReturnError(failureErr)
		return failureErr
	}
	link.WillReturnResult(sqlmock.NewResult(1, 1))
	outbox := mock.ExpectExec("INSERT INTO scheduler_outbox").
		WithArgs(service.SchedulerOutboxEventAccountChanged, int64(41), nil, []byte(`{"group_ids":[11]}`), sqlmock.AnyArg())
	if failure == "outbox" {
		outbox.WillReturnError(failureErr)
		return failureErr
	}
	outbox.WillReturnResult(sqlmock.NewResult(1, 1))
	if failure == "commit" {
		mock.ExpectCommit().WillReturnError(failureErr)
		return failureErr
	}
	mock.ExpectCommit()
	return nil
}

func TestSharedPoolReimportDeletedCreatesNewAccountAndOwner(t *testing.T) {
	for _, ownerID := range []int64{7, 19} {
		name := "same owner"
		if ownerID != 7 {
			name = "different owner"
		}
		t.Run(name, func(t *testing.T) {
			f := newSharedImportFixture(t)
			f.group.IsExclusive = false
			f.account.Extra[service.SharedPoolOwnerKey] = ownerID
			f.mock.ExpectBegin()
			expectSharedImportGroup(f.mock, f.group)
			expectSharedReimportDeleted(f.mock)
			expectSharedReimportWrites(f.mock, ownerID, "")
			require.NoError(t, f.repo.CreateSharedAccount(context.Background(), f.account, ownerID, "fixture-fingerprint"))
			require.Equal(t, int64(41), f.account.ID)
			require.NotEqual(t, int64(17), f.account.ID)
			require.Equal(t, ownerID, f.account.Extra[service.SharedPoolOwnerKey])
		})
	}
}

func TestSharedPoolReimportRejectsUndeletedAccount(t *testing.T) {
	f := newSharedImportFixture(t)
	f.group.IsExclusive = false
	f.mock.ExpectBegin()
	expectSharedImportGroup(f.mock, f.group)
	// deleted_at 是唯一放行条件，停用、管理员禁用都不能代替删除。
	f.mock.ExpectQuery(regexp.QuoteMeta(sharedReimportLookup)).WithArgs("fixture-fingerprint").
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "deleted_at"}).AddRow(17, nil)).RowsWillBeClosed()
	f.mock.ExpectRollback()
	err := f.repo.CreateSharedAccount(context.Background(), f.account, 19, "fixture-fingerprint")
	require.Equal(t, "SHARED_ACCOUNT_EXISTS", infraerrors.Reason(err))
	require.Zero(t, f.account.ID)
}

func TestSharedPoolReimportRollsBackReleasedFingerprintOnWriteFailure(t *testing.T) {
	for _, failure := range []string{"account", "ownership", "association", "outbox", "commit"} {
		t.Run(failure, func(t *testing.T) {
			f := newSharedImportFixture(t)
			f.group.IsExclusive = false
			f.mock.ExpectBegin()
			expectSharedImportGroup(f.mock, f.group)
			expectSharedReimportDeleted(f.mock)
			wantErr := expectSharedReimportWrites(f.mock, 19, failure)
			if failure != "commit" {
				f.mock.ExpectRollback()
			}
			err := f.repo.CreateSharedAccount(context.Background(), f.account, 19, "fixture-fingerprint")
			require.ErrorIs(t, err, wantErr)
		})
	}
}

func TestSharedPoolReimportLookupAndReleaseFailuresRollBack(t *testing.T) {
	for _, failure := range []string{"query", "scan", "rows", "close", "release"} {
		t.Run(failure, func(t *testing.T) {
			f := newSharedImportFixture(t)
			f.group.IsExclusive = false
			f.mock.ExpectBegin()
			expectSharedImportGroup(f.mock, f.group)
			wantErr := errors.New("fixture " + failure + " failure")
			lookup := f.mock.ExpectQuery(regexp.QuoteMeta(sharedReimportLookup)).WithArgs("fixture-fingerprint")
			rows := sqlmock.NewRows([]string{"account_id", "deleted_at"})
			switch failure {
			case "query":
				lookup.WillReturnError(wantErr)
			case "scan":
				lookup.WillReturnRows(rows.AddRow("invalid-id", time.Now())).RowsWillBeClosed()
			case "rows":
				lookup.WillReturnRows(rows.AddRow(17, time.Now()).RowError(0, wantErr)).RowsWillBeClosed()
			case "close":
				lookup.WillReturnRows(rows.AddRow(17, time.Now()).CloseError(wantErr)).RowsWillBeClosed()
			case "release":
				lookup.WillReturnRows(rows.AddRow(17, time.Now())).RowsWillBeClosed()
				f.mock.ExpectExec(regexp.QuoteMeta(`UPDATE shared_pool_accounts SET credential_fingerprint=$2,updated_at=NOW() WHERE account_id=$1`)).
					WithArgs(int64(17), "deleted:17").WillReturnError(wantErr)
			}
			f.mock.ExpectRollback()
			err := f.repo.CreateSharedAccount(context.Background(), f.account, 19, "fixture-fingerprint")
			if failure == "scan" {
				require.ErrorContains(t, err, "invalid-id")
			} else {
				require.ErrorIs(t, err, wantErr)
			}
			require.Zero(t, f.account.ID)
		})
	}
}

func TestSharedPoolReimportUniqueRaceRollsBackNewAccount(t *testing.T) {
	f := newSharedImportFixture(t)
	f.group.IsExclusive = false
	f.mock.ExpectBegin()
	expectSharedImportGroup(f.mock, f.group)
	expectSharedCredentialAbsent(f.mock)
	f.mock.ExpectQuery(`INSERT INTO "accounts"`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(41))
	f.mock.ExpectExec("INSERT INTO shared_pool_accounts").WithArgs(int64(41), int64(19), "fixture-fingerprint", true, true).
		WillReturnError(&pq.Error{Code: "23505", Constraint: "shared_pool_accounts_credential_fingerprint_key"})
	f.mock.ExpectRollback()
	err := f.repo.CreateSharedAccount(context.Background(), f.account, 19, "fixture-fingerprint")
	require.Equal(t, "SHARED_ACCOUNT_EXISTS", infraerrors.Reason(err))
}
