package repository

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type astraStoredPolicyArgument struct{ expected config.AstraRoutingSettings }

func (a astraStoredPolicyArgument) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	var actual config.AstraRoutingSettings
	return json.Unmarshal([]byte(raw), &actual) == nil && reflect.DeepEqual(actual, a.expected)
}

func expectAstraGroupSaveStart(mock sqlmock.Sqlmock, members []int64) {
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT key FROM settings WHERE key=\$1 FOR UPDATE`).WithArgs("astra-routing-test").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`account_group_live_group_lock`).WithArgs(pq.Array([]int64{11})).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	mock.ExpectQuery(`(?s)astra_selection_account_lock.*ORDER BY id FOR UPDATE`).WithArgs(pq.Array([]int64{101, 202})).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(101).AddRow(202))
	expectAstraGroupAccounts(mock, []int64{11}, members)
}

func TestAstraGroupSavePersistsPolicyWithoutMemberSnapshot(t *testing.T) {
	repo, _, mock := newAstraGroupSelectionRepo(t)
	value := astraGroupSelectionSettings()
	value.CookiePool.SourceAccountIDs = []int64{101}
	value.SelectionError = "old_preview_error"
	expectAstraGroupSaveStart(mock, []int64{101})
	mock.ExpectQuery(`SELECT value FROM settings WHERE key=\$1`).WithArgs("astra-routing-test").WillReturnRows(sqlmock.NewRows([]string{"value"}))
	for _, id := range []int64{101, 202} {
		mock.ExpectExec(`UPDATE accounts SET`).WithArgs(id, false).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnResult(sqlmock.NewResult(1, 1))
	}
	expected := value
	expected.CookiePool.SourceAccountIDs = nil
	expected.SelectionError = ""
	mock.ExpectQuery(`INSERT INTO "settings"`).WithArgs("astra-routing-test", astraStoredPolicyArgument{expected}, sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()
	require.NoError(t, repo.SetAstraRoutingWithAccounts(t.Context(), "astra-routing-test", value))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstraGroupSaveRechecksMembershipAfterAccountLock(t *testing.T) {
	repo, _, mock := newAstraGroupSelectionRepo(t)
	value := astraGroupSelectionSettings()
	value.CookiePool.SourceAccountIDs = []int64{101}
	expectAstraGroupSaveStart(mock, []int64{102})
	mock.ExpectRollback()
	require.ErrorContains(t, repo.SetAstraRoutingWithAccounts(t.Context(), "astra-routing-test", value), "astra_group_membership_changed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstraGroupSaveSchedulingOnlyDoesNotUpdateAccounts(t *testing.T) {
	repo, _, mock := newAstraGroupSelectionRepo(t)
	value := astraGroupSelectionSettings()
	value.CookiePool.SourceAccountIDs = []int64{101}
	value.WSSession = config.CodexWSAnchorConfig{Enabled: true, AccountIDs: []int64{202}}
	previous, err := json.Marshal(config.AstraStoredSettings(value))
	require.NoError(t, err)
	value.AccountScheduling = true
	value.SchedulingMode = "model"
	expectAstraGroupSaveStart(mock, []int64{101})
	mock.ExpectQuery(`SELECT value FROM settings WHERE key=\$1`).WithArgs("astra-routing-test").WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(previous))
	// 无账号 UPDATE / outbox 预期，任何映射或 WS 重写都会导致此测试失败。
	mock.ExpectQuery(`INSERT INTO "settings"`).WithArgs("astra-routing-test", astraStoredPolicyArgument{config.AstraStoredSettings(value)}, sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
	mock.ExpectCommit()
	require.NoError(t, repo.SetAstraRoutingWithAccounts(t.Context(), "astra-routing-test", value))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstraGroupSaveRollsBackAccountLockFailure(t *testing.T) {
	repo, _, mock := newAstraGroupSelectionRepo(t)
	value := astraGroupSelectionSettings()
	value.CookiePool.SourceAccountIDs = []int64{101}
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT key FROM settings`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`account_group_live_group_lock`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(11))
	failure := errors.New("account lock interrupted")
	mock.ExpectQuery(`astra_selection_account_lock`).WithArgs(pq.Array([]int64{101, 202})).WillReturnError(failure)
	mock.ExpectRollback()
	require.ErrorIs(t, repo.SetAstraRoutingWithAccounts(t.Context(), "astra-routing-test", value), failure)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstraGroupSaveRollsBackMissingGroup(t *testing.T) {
	repo, _, mock := newAstraGroupSelectionRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT key FROM settings`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`account_group_live_group_lock`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
	err := repo.SetAstraRoutingWithAccounts(t.Context(), "astra-routing-test", astraGroupSelectionSettings())
	require.ErrorContains(t, err, "astra_group_unavailable")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstraGroupSaveRollsBackPartialAccountUpdates(t *testing.T) {
	repo, _, mock := newAstraGroupSelectionRepo(t)
	value := astraGroupSelectionSettings()
	value.CookiePool.SourceAccountIDs = []int64{101}
	expectAstraGroupSaveStart(mock, []int64{101})
	mock.ExpectQuery(`SELECT value FROM settings`).WillReturnRows(sqlmock.NewRows([]string{"value"}))
	mock.ExpectExec(`UPDATE accounts SET`).WithArgs(int64(101), false).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnResult(sqlmock.NewResult(1, 1))
	failure := errors.New("target update failed")
	mock.ExpectExec(`UPDATE accounts SET`).WithArgs(int64(202), false).WillReturnError(failure)
	mock.ExpectRollback()
	require.ErrorIs(t, repo.SetAstraRoutingWithAccounts(t.Context(), "astra-routing-test", value), failure)
	require.NoError(t, mock.ExpectationsWereMet())
}

func astraGroupSchedulingSettings() config.AstraRoutingSettings {
	value := astraGroupSelectionSettings()
	value.AccountScheduling = true
	value.CookiePool.SourceAccountIDs = []int64{101}
	value.CookiePool.TargetSelection = "groups"
	value.CookiePool.TargetGroupIDs = []int64{12}
	return value
}

func expectAstraGroupSchedulingStart(t *testing.T, mock sqlmock.Sqlmock, value config.AstraRoutingSettings, schedulable bool) {
	t.Helper()
	raw, err := json.Marshal(config.AstraStoredSettings(value))
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT value FROM settings.*FOR SHARE`).WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(raw))
	mock.ExpectQuery(`(?s)SELECT credentials.*FROM accounts.*FOR UPDATE`).WithArgs(int64(202)).WillReturnRows(sqlmock.NewRows([]string{"mapping", "extra", "schedulable", "updated_at", "eligible"}).AddRow(nil, []byte(`{}`), schedulable, time.Now(), true))
	expectAstraGroupAccounts(mock, []int64{11}, []int64{101})
}

func TestAstraGroupSchedulingUsesCurrentMembership(t *testing.T) {
	for _, moved := range []bool{false, true} {
		t.Run(map[bool]string{false: "current member", true: "moved out"}[moved], func(t *testing.T) {
			settingsRepo, db, mock := newAstraGroupSelectionRepo(t)
			repo := newAccountRepositoryWithSQL(settingsRepo.client, db, nil)
			value := astraGroupSchedulingSettings()
			expectAstraGroupSchedulingStart(t, mock, value, true)
			target := int64(202)
			if moved {
				target = 303
			}
			expectAstraGroupAccounts(mock, []int64{12}, []int64{target})
			if moved {
				mock.ExpectRollback()
			} else {
				mock.ExpectQuery(`SELECT state FROM astra_scheduling_states`).WillReturnRows(sqlmock.NewRows([]string{"state"}))
				mock.ExpectQuery(`SELECT COALESCE\(jsonb_agg`).WillReturnRows(sqlmock.NewRows([]string{"groups"}).AddRow([]byte(`[]`)))
				mock.ExpectCommit()
			}
			result, err := repo.ApplyAstraScheduling(t.Context(), 202, value, true)
			if moved {
				require.ErrorContains(t, err, "configuration_changed")
			} else {
				require.NoError(t, err)
				require.True(t, result.Allowed)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestAstraGroupSchedulingRestoresPausedMember(t *testing.T) {
	settingsRepo, db, mock := newAstraGroupSelectionRepo(t)
	repo := newAccountRepositoryWithSQL(settingsRepo.client, db, nil)
	value := astraGroupSchedulingSettings()
	expectAstraGroupSchedulingStart(t, mock, value, false)
	expectAstraGroupAccounts(mock, []int64{12}, []int64{202})
	mock.ExpectQuery(`SELECT state FROM astra_scheduling_states`).WithArgs(int64(202)).WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow([]byte(`{"mode":"account"}`)))
	mock.ExpectQuery(`SELECT COALESCE\(jsonb_agg`).WithArgs(int64(202)).WillReturnRows(sqlmock.NewRows([]string{"groups"}).AddRow([]byte(`[]`)))
	mock.ExpectExec(`UPDATE accounts SET schedulable=true`).WithArgs(int64(202)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM astra_scheduling_states`).WithArgs(int64(202)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`UPDATE accounts SET updated_at=clock_timestamp\(\)`).WithArgs(int64(202)).WillReturnRows(sqlmock.NewRows([]string{"updated_at"}).AddRow(time.Now()))
	mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	result, err := repo.ApplyAstraScheduling(t.Context(), 202, value, true)
	require.NoError(t, err)
	require.True(t, result.Allowed)
	require.True(t, result.Changed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestAstraGroupSchedulingRetainsRevisionFence(t *testing.T) {
	settingsRepo, db, mock := newAstraGroupSelectionRepo(t)
	repo := newAccountRepositoryWithSQL(settingsRepo.client, db, nil)
	value := astraGroupSchedulingSettings()
	raw, err := json.Marshal(config.AstraStoredSettings(value))
	require.NoError(t, err)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT value FROM settings.*FOR SHARE`).WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(raw))
	mock.ExpectRollback()
	value.Revision = "stale"
	_, err = repo.ApplyAstraScheduling(t.Context(), 202, value, true)
	require.ErrorContains(t, err, "configuration_changed")
	require.NoError(t, mock.ExpectationsWereMet())
}
