package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketStrategyFullUpdatePreservesLatestUnlessExplicit(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		r, mock := newCodexTicketCASRepo(t)
		mock.ExpectBegin()
		expectTicketProxyFullUpdateLocks(mock, `{"codex_ticket_proxy_mode":"random","codex_ticket_proxy_strategy":"round_robin"}`)
		mock.ExpectExec(`(?s)UPDATE .*accounts.*SET.*WHERE .*id.*`).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`(?s)SELECT .* FROM "accounts" WHERE "id" = \$1`).WithArgs(int64(27)).WillReturnRows(updatedAccountRows(27, `{}`))
		mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		account := &service.Account{ID: 27, Name: "synced", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
			Credentials: map[string]any{"access_token": "test"}, Extra: map[string]any{service.CodexTicketProxyStrategyExtraKey: "affinity"},
			Concurrency: 1, Priority: 1, Status: service.StatusActive, Schedulable: true}
		ctx := context.Background()
		want := "round_robin"
		if explicit {
			ctx = service.WithCodexTicketProxyWrite(ctx, account.Extra)
			want = "affinity"
		}
		require.NoError(t, r.Update(ctx, account))
		require.Equal(t, want, account.CodexTicketProxyStrategy())
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestCodexTicketStrategyCASAndSQLPreservation(t *testing.T) {
	before := codexTicketCASConfig(map[string]any{service.CodexTicketProxyStrategyExtraKey: "affinity"})
	after := codexTicketCASConfig(map[string]any{service.CodexTicketProxyStrategyExtraKey: "round_robin"})
	require.NotEqual(t, before, after)
	require.Contains(t, preserveCodexTicketProxyExtraSQL("$1::jsonb"), "codex_ticket_proxy_strategy")
	require.True(t, needsCodexTicketProxyTransaction(context.Background(), map[string]any{service.CodexTicketProxyStrategyExtraKey: "round_robin"}))
}
