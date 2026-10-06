package repository

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func expectAstraMixedSelectionSave(t *testing.T, mock sqlmock.Sqlmock) config.AstraRoutingSettings {
	t.Helper()
	previous := astraGroupSelectionSettings()
	previous.CookiePool.TargetAccountIDs = []int64{202, 303}
	previous.WSSession = config.CodexWSAnchorConfig{Enabled: true, AccountIDs: []int64{303}}
	raw, err := json.Marshal(config.AstraStoredSettings(previous))
	require.NoError(t, err)
	value := previous
	value.CookiePool.SourceAccountIDs = []int64{101, 202}
	value.CookiePool.TargetAccountIDs = []int64{303}
	value.AccountScheduling = true
	value.SchedulingMode = "model"
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT key FROM settings`).WithArgs("astra-routing-test").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`account_group_live_group_lock`).WithArgs(pq.Array([]int64{11})).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	mock.ExpectQuery(`(?s)astra_selection_account_lock.*ORDER BY id FOR UPDATE`).WithArgs(pq.Array([]int64{101, 202, 303})).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(101).AddRow(202).AddRow(303))
	expectAstraGroupAccounts(mock, []int64{11}, []int64{101, 202})
	mock.ExpectQuery(`SELECT value FROM settings WHERE key=\$1`).WithArgs("astra-routing-test").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(raw)).RowsWillBeClosed()
	return value
}

func TestAstraGroupSaveSchedulingOnlyWithExcludedManualTargetsDoesNotUpdateAccounts(t *testing.T) {
	repo, _, mock := newAstraGroupSelectionRepo(t)
	value := expectAstraMixedSelectionSave(t, mock)
	expectAstraGroupAccounts(mock, []int64{11}, []int64{101, 202})
	// 旧手选目标含新来源账号，但有效路由不变；不得重写账号映射、WS 或 outbox。
	mock.ExpectQuery(`INSERT INTO "settings"`).WithArgs("astra-routing-test", astraStoredPolicyArgument{config.AstraStoredSettings(value)}, sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()
	require.NoError(t, repo.SetAstraRoutingWithAccounts(t.Context(), "astra-routing-test", value))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstraGroupSaveRollsBackPreviousPolicyResolutionFailure(t *testing.T) {
	repo, _, mock := newAstraGroupSelectionRepo(t)
	value := expectAstraMixedSelectionSave(t, mock)
	failure := errors.New("previous policy read failed")
	expectAstraSelectionGroups(mock, 11).WillReturnError(failure)
	mock.ExpectRollback()
	require.ErrorIs(t, repo.SetAstraRoutingWithAccounts(t.Context(), "astra-routing-test", value), failure)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstraGroupSavePreparationComparisonKeepsRevisionGuard(t *testing.T) {
	_, db, mock := newAstraGroupSelectionRepo(t)
	previous := astraGroupSelectionSettings()
	previous.CookiePool.TargetAccountIDs = []int64{202, 303}
	value := previous
	value.CookiePool.SourceAccountIDs = []int64{101, 202}
	value.CookiePool.TargetAccountIDs = []int64{303}
	value.Revision = "changed-policy"
	unchanged, err := astraRoutePreparationUnchanged(t.Context(), db, previous, value)
	require.NoError(t, err)
	require.False(t, unchanged)
	require.NoError(t, mock.ExpectationsWereMet())
}
