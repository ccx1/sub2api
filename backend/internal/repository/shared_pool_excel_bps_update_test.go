//go:build unit

package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

const sharedExcelBPSUpdateSQL = `UPDATE accounts SET extra=(COALESCE(extra,'{}'::jsonb)-$2::text[])||$3::jsonb WHERE id=$1`

func TestSharedPoolUpdateReplacesExcelBPSFamilyOnlyWhenChanged(t *testing.T) {
	for _, tc := range []struct {
		name      string
		changed   bool
		extra     map[string]any
		patchJSON string
	}{
		{name: "omitted keeps existing bps"},
		{name: "disabled removes family", changed: true, patchJSON: `{}`},
		{name: "enabled with options", changed: true, extra: map[string]any{
			"openai_excel_bps": true, "openai_excel_bps_models": []string{"gpt-6-astra"}, "openai_excel_bps_auto_disable_on_403": true,
		}, patchJSON: `{"openai_excel_bps":true,"openai_excel_bps_auto_disable_on_403":true,"openai_excel_bps_models":["gpt-6-astra"]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newSharedDailyCooldownRepository(t)
			in := service.SharedPoolAccountUpdate{Name: "updated", Concurrency: 7, ExcelBPSChanged: tc.changed, ExcelBPSExtra: tc.extra}
			expectSharedDailyCooldownLock(mock).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(41))
			expectSharedDailyCooldownProfile(mock, in)
			if tc.changed {
				mock.ExpectExec(regexp.QuoteMeta(sharedExcelBPSUpdateSQL)).
					WithArgs(int64(41), pq.Array(service.ExcelBPSExtraKeys()), tc.patchJSON).
					WillReturnResult(sqlmock.NewResult(0, 1))
			}
			expectSharedDailyCooldownOutbox(mock).WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()
			require.NoError(t, repo.UpdateSharedAccount(context.Background(), 7, 41, in))
		})
	}
}
