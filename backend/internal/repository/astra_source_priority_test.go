package repository

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAstraSourcePrioritySchedulingChecksResolvedManualTargets(t *testing.T) {
	for _, stale := range []bool{false, true} {
		t.Run(map[bool]string{false: "valid exclusion", true: "changed target policy"}[stale], func(t *testing.T) {
			settingsRepo, db, mock := newAstraGroupSelectionRepo(t)
			repo := newAccountRepositoryWithSQL(settingsRepo.client, db, nil)
			stored := astraGroupSelectionSettings()
			stored.AccountScheduling = true
			stored.CookiePool.TargetAccountIDs = []int64{202, 303}
			raw, err := json.Marshal(config.AstraStoredSettings(stored))
			require.NoError(t, err)
			runtime := stored
			runtime.CookiePool.SourceAccountIDs = []int64{101, 202}
			runtime.CookiePool.TargetAccountIDs = []int64{303}
			if stale {
				runtime.CookiePool.TargetAccountIDs = []int64{303, 404}
			}
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT value FROM settings.*FOR SHARE`).WillReturnRows(sqlmock.NewRows([]string{"value"}).AddRow(raw))
			mock.ExpectQuery(`(?s)SELECT credentials.*FROM accounts.*FOR UPDATE`).WithArgs(int64(303)).WillReturnRows(sqlmock.NewRows([]string{"mapping", "extra", "schedulable", "updated_at", "eligible"}).AddRow(nil, []byte(`{}`), true, time.Now(), true))
			expectAstraGroupAccounts(mock, []int64{11}, []int64{101, 202})
			if stale {
				mock.ExpectRollback()
			} else {
				mock.ExpectQuery(`SELECT state FROM astra_scheduling_states`).WillReturnRows(sqlmock.NewRows([]string{"state"}))
				mock.ExpectQuery(`SELECT COALESCE\(jsonb_agg`).WillReturnRows(sqlmock.NewRows([]string{"groups"}).AddRow([]byte(`[]`)))
				mock.ExpectCommit()
			}
			_, err = repo.ApplyAstraScheduling(t.Context(), 303, runtime, true)
			if stale {
				require.ErrorContains(t, err, "configuration_changed")
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
