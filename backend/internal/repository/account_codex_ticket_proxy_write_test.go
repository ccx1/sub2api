package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestTicketProxyFullUpdateUsesExplicitIntentAndLatestLockedConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name, current     string
		incoming, request map[string]any
		wantMode          string
		wantID            int64
		valid             bool
	}{
		{"CRS stale random snapshot", `{"codex_ticket_proxy_mode":"fixed","codex_ticket_proxy_id":29}`, map[string]any{"codex_ticket_proxy_mode": "random", "codex_ticket_proxy_id": 0}, nil, "fixed", 29, true},
		{"refresh stale fixed snapshot", `{"codex_ticket_proxy_mode":"fixed","codex_ticket_proxy_id":29}`, map[string]any{"codex_ticket_proxy_mode": "fixed", "codex_ticket_proxy_id": 2}, nil, "fixed", 29, true},
		{"unrelated admin edit keeps newer selection", `{"codex_ticket_proxy_mode":"random","codex_ticket_proxy_id":0}`, map[string]any{"codex_ticket_proxy_mode": "fixed", "codex_ticket_proxy_id": 2}, map[string]any{"other": true}, "random", 0, true},
		{"explicit fixed override", `{"codex_ticket_proxy_mode":"random","codex_ticket_proxy_id":0}`, map[string]any{"codex_ticket_proxy_mode": "fixed", "codex_ticket_proxy_id": 17}, map[string]any{"codex_ticket_proxy_mode": "fixed", "codex_ticket_proxy_id": 17}, "fixed", 17, true},
		{"explicit random clears stale id", `{"codex_ticket_proxy_mode":"fixed","codex_ticket_proxy_id":29}`, map[string]any{"codex_ticket_proxy_mode": "random", "codex_ticket_proxy_id": 2}, map[string]any{"codex_ticket_proxy_mode": "random"}, "random", 0, true},
		{"partial id checks locked mode", `{"codex_ticket_proxy_mode":"random","codex_ticket_proxy_id":0}`, map[string]any{"codex_ticket_proxy_mode": "fixed", "codex_ticket_proxy_id": 17}, map[string]any{"codex_ticket_proxy_id": 17}, "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, mock := newCodexTicketCASRepo(t)
			mock.ExpectBegin()
			expectTicketProxyFullUpdateLocks(mock, tc.current)
			if tc.valid {
				mock.ExpectExec(`(?s)UPDATE .*accounts.*SET.*WHERE .*id.*`).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectQuery(`(?s)SELECT .* FROM "accounts" WHERE "id" = \$1`).WithArgs(int64(27)).WillReturnRows(updatedAccountRows(27, `{}`))
				mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			a := &service.Account{ID: 27, Name: "synced", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "test"}, Extra: tc.incoming, Concurrency: 1,
				Priority: 1, Status: service.StatusActive, Schedulable: true}
			ctx := context.Background()
			if tc.request != nil {
				ctx = service.WithCodexTicketProxyWrite(ctx, tc.request)
			}
			err := r.Update(ctx, a)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, tc.wantMode, a.CodexTicketProxyMode())
				require.Equal(t, tc.wantID, a.CodexTicketProxyID())
			} else {
				require.Error(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func expectTicketProxyFullUpdateLocks(mock sqlmock.Sqlmock, current string) {
	mock.ExpectQuery(`(?s)SELECT.*FOR NO KEY UPDATE`).
		WithArgs(int64(27), service.PlatformOpenAI, service.AccountTypeOAuth, `{"access_token":"test"}`, nil).
		WillReturnRows(sqlmock.NewRows([]string{"identity_unchanged", "ollama_group_unchanged", "ollama_proxy_unchanged", "enabled", "rate_sync_enabled", "snapshot", "ollama_session", "ollama_auto", "ollama_snapshot", "current_extra"}).
			AddRow(true, false, true, nil, nil, nil, nil, nil, nil, []byte(current)))
	mock.ExpectQuery(`SELECT extra FROM accounts WHERE id = \$1 AND deleted_at IS NULL`).WithArgs(int64(27)).
		WillReturnRows(sqlmock.NewRows([]string{"extra"}).AddRow([]byte(current)))
}
