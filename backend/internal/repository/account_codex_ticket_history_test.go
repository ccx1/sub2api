package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type ticketHistoryPayload struct{ total, success, failed int64 }

func (want ticketHistoryPayload) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	var history service.CodexTicketHistory
	return json.Unmarshal([]byte(raw), &history) == nil && history.Summary.Total == want.total &&
		history.Summary.Success == want.success && history.Summary.Failed == want.failed && len(history.Items) == 99
}

func TestCodexTicketHistoryAppendsUnderRowLockPrunesExpiredRowsAndPreservesOtherExtra(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	history := service.CodexTicketHistory{}
	base := time.Now().Add(-2 * time.Hour)
	history.Append(service.CodexTicketAttempt{ID: "expired-1", StartedAt: base.Add(-11 * time.Hour), Success: true})
	history.Append(service.CodexTicketAttempt{ID: "expired-2", StartedAt: base.Add(-11 * time.Hour), Success: true})
	for i := 0; i < 98; i++ {
		history.Append(service.CodexTicketAttempt{StartedAt: base.Add(time.Duration(i) * time.Minute), Success: true})
	}
	previous, err := json.Marshal(map[string]any{service.OpenAICodexTicketHistoryKey: history})
	require.NoError(t, err)
	attempt := service.CodexTicketAttempt{ID: "test-attempt", StartedAt: time.Now(), FinishedAt: time.Now(), Success: false}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT extra FROM accounts WHERE id = \$1 AND deleted_at IS NULL FOR NO KEY UPDATE`).
		WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"history"}).AddRow(previous))
	mock.ExpectExec(`UPDATE accounts SET extra = COALESCE\(extra, '\{\}'::jsonb\) \|\| jsonb_build_object\(\$2::text, \$3::jsonb\) WHERE id = \$1 AND deleted_at IS NULL`).
		WithArgs(int64(41), service.OpenAICodexTicketHistoryKey, ticketHistoryPayload{101, 100, 1}).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.RecordCodexTicketAttempt(context.Background(), 41, attempt))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexTicketHistoryFailureRollsBackWithoutOverwriting(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT extra`).WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"history"}).AddRow(nil))
	mock.ExpectExec(`UPDATE accounts SET extra`).WillReturnError(errors.New("write failed"))
	mock.ExpectRollback()
	err := repo.RecordCodexTicketAttempt(context.Background(), 41, service.CodexTicketAttempt{ID: "test", StartedAt: time.Now()})
	require.ErrorContains(t, err, "write failed")
	require.NoError(t, mock.ExpectationsWereMet())
}
