//go:build unit

package repository

import (
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolPrismUpdateUsesOwnedTransaction(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
			repo, mock := newSharedDailyCooldownRepository(t)
			models := []string{}
			patch := `{"openai_prism_browser":false,"openai_prism_browser_models":[]}`
			if enabled {
				models = []string{"gpt-6.1-sol"}
				patch = `{"openai_prism_browser":true,"openai_prism_browser_models":["gpt-6.1-sol"]}`
			}
			in := service.SharedPoolAccountUpdate{Name: "updated", Concurrency: 7, PrismBrowserChanged: true,
				PrismBrowserExtra: map[string]any{service.PrismBrowserEnabledKey: enabled, service.PrismBrowserModelsKey: models}}
			expectSharedDailyCooldownLock(mock).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(41))
			expectSharedDailyCooldownProfile(mock, in)
			mock.ExpectExec(regexp.QuoteMeta(sharedExcelBPSUpdateSQL)).WithArgs(int64(41), pq.Array(service.PrismBrowserExtraKeys()), patch).WillReturnResult(sqlmock.NewResult(0, 1))
			expectSharedDailyCooldownOutbox(mock).WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()
			require.NoError(t, repo.UpdateSharedAccount(t.Context(), 7, 41, in))
		})
	}
}
