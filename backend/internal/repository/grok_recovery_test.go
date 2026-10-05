package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGrokRecoveryCASGuardsObservedState(t *testing.T) {
	for _, changed := range []int64{0, 1} {
		repo, mock := newCodexTicketCASRepo(t)
		until := time.Now().Add(time.Minute)
		mock.ExpectExec(`(?s)WITH updated AS.*status = 'active'.*schedulable IS TRUE.*temp_unschedulable_until = \$2.*temp_unschedulable_reason = \$3.*INSERT INTO scheduler_outbox`).
			WithArgs(int64(41), until, "grok credentials unauthorized", service.SchedulerOutboxEventAccountChanged).
			WillReturnResult(sqlmock.NewResult(0, changed))
		applied, err := repo.ClearTempUnschedulableIfUnchanged(context.Background(), 41, until, "grok credentials unauthorized")
		require.NoError(t, err)
		require.Equal(t, changed == 1, applied)
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestGrokModelRecoveryCASOnlyDeletesObservedModel(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	mock.ExpectExec(regexp.QuoteMeta(clearGrokModelRateLimitCAS)).
		WithArgs(int64(41), "grok-4.5", `{"reason":"grok model quota exhausted"}`, service.SchedulerOutboxEventAccountChanged).
		WillReturnResult(sqlmock.NewResult(0, 0))
	applied, err := repo.ClearGrokModelRateLimitIfUnchanged(context.Background(), 41, "grok-4.5", map[string]any{"reason": "grok model quota exhausted"})
	require.NoError(t, err)
	require.False(t, applied, "newer model state is retained when JSON snapshot changed")
	require.Contains(t, clearGrokModelRateLimitCAS, "status = 'active'")
	require.Contains(t, clearGrokModelRateLimitCAS, "AND schedulable IS TRUE")
	require.NoError(t, mock.ExpectationsWereMet())
}
