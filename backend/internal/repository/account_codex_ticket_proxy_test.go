package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func expectTicketProxyLock(mock sqlmock.Sqlmock, ids, current string) {
	mock.ExpectQuery(`SELECT extra FROM accounts WHERE id = ANY\(\$1\) AND deleted_at IS NULL ORDER BY id FOR NO KEY UPDATE`).
		WithArgs(ids).WillReturnRows(sqlmock.NewRows([]string{"extra"}).AddRow([]byte(current)))
}

func TestTicketProxyPartialUpdateValidatesUnderTransactionLock(t *testing.T) {
	for _, tc := range []struct {
		name, current, payload string
		patch                  map[string]any
		valid                  bool
	}{
		{"id only keeps fixed", `{"codex_ticket_proxy_mode":"fixed","codex_ticket_proxy_id":2}`, `{"codex_ticket_proxy_id":8}`, map[string]any{"codex_ticket_proxy_id": 8}, true},
		{"mode only keeps fixed id", `{"codex_ticket_proxy_mode":"fixed","codex_ticket_proxy_id":2}`, `{"codex_ticket_proxy_mode":"fixed"}`, map[string]any{"codex_ticket_proxy_mode": "fixed"}, true},
		{"inherit clears fixed id", `{"codex_ticket_proxy_mode":"fixed","codex_ticket_proxy_id":2}`, `{"codex_ticket_proxy_id":0,"codex_ticket_proxy_mode":"inherit"}`, map[string]any{"codex_ticket_proxy_mode": "inherit"}, true},
		{"account clears fixed id", `{"codex_ticket_proxy_mode":"fixed","codex_ticket_proxy_id":2}`, `{"codex_ticket_proxy_id":0,"codex_ticket_proxy_mode":"account"}`, map[string]any{"codex_ticket_proxy_mode": "account"}, true},
		{"account rejects stale fixed id", `{"codex_ticket_proxy_mode":"account","codex_ticket_proxy_id":0}`, "", map[string]any{"codex_ticket_proxy_id": 8}, false},
		{"random clears fixed id", `{"codex_ticket_proxy_mode":"fixed","codex_ticket_proxy_id":2}`, `{"codex_ticket_proxy_id":0,"codex_ticket_proxy_mode":"random"}`, map[string]any{"codex_ticket_proxy_mode": "random"}, true},
		{"id invalid after concurrent mode change", `{"codex_ticket_proxy_mode":"random","codex_ticket_proxy_id":0}`, "", map[string]any{"codex_ticket_proxy_id": 8}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, mock := newCodexTicketCASRepo(t)
			mock.ExpectBegin()
			expectTicketProxyLock(mock, "{7}", tc.current)
			if tc.valid {
				mock.ExpectExec(`UPDATE accounts SET extra`).WithArgs(tc.payload, int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err := r.UpdateExtra(context.Background(), 7, tc.patch)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestTicketProxyUpdateReusesCallerTransaction(t *testing.T) {
	r, mock := newCodexTicketCASRepo(t)
	mock.ExpectBegin()
	tx, err := r.client.Tx(context.Background())
	require.NoError(t, err)
	expectTicketProxyLock(mock, "{7}", `{"codex_ticket_proxy_mode":"fixed","codex_ticket_proxy_id":2}`)
	mock.ExpectExec(`UPDATE accounts SET extra`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnResult(sqlmock.NewResult(1, 1))
	require.NoError(t, r.UpdateExtra(dbent.NewTxContext(context.Background(), tx), 7, map[string]any{"codex_ticket_proxy_mode": "random"}))
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback(), "repository must leave caller transaction open")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketProxyUpdateRollsBackWhenOutboxFails(t *testing.T) {
	r, mock := newCodexTicketCASRepo(t)
	mock.ExpectBegin()
	expectTicketProxyLock(mock, "{7}", `{}`)
	mock.ExpectExec(`UPDATE accounts SET extra`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnError(errors.New("outbox failed"))
	mock.ExpectRollback()
	err := r.UpdateExtra(context.Background(), 7, map[string]any{"codex_ticket_proxy_mode": "random"})
	require.EqualError(t, err, "outbox failed")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketProxyBulkValidatesEveryLockedAccountBeforeWriting(t *testing.T) {
	r, mock := newCodexTicketCASRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT extra FROM accounts.*ORDER BY id FOR NO KEY UPDATE`).WithArgs("{8,7}").WillReturnRows(
		sqlmock.NewRows([]string{"extra"}).AddRow([]byte(`{"codex_ticket_proxy_mode":"fixed","codex_ticket_proxy_id":2}`)).AddRow([]byte(`{"codex_ticket_proxy_mode":"random","codex_ticket_proxy_id":0}`)))
	mock.ExpectRollback()
	count, err := r.BulkUpdate(context.Background(), []int64{8, 7}, service.AccountBulkUpdate{Extra: map[string]any{"codex_ticket_proxy_mode": "fixed"}})
	require.Error(t, err)
	require.Zero(t, count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketProxyBulkCommitsOneAtomicPatch(t *testing.T) {
	r, mock := newCodexTicketCASRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT extra FROM accounts.*ORDER BY id FOR NO KEY UPDATE`).WithArgs("{8,7}").WillReturnRows(
		sqlmock.NewRows([]string{"extra"}).AddRow([]byte(`{"codex_ticket_proxy_mode":"fixed","codex_ticket_proxy_id":2}`)).AddRow([]byte(`{}`)))
	mock.ExpectExec(`UPDATE accounts SET extra`).WithArgs([]byte(`{"codex_ticket_proxy_id":0,"codex_ticket_proxy_mode":"random"}`), "{8,7}").WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	count, err := r.BulkUpdate(context.Background(), []int64{8, 7}, service.AccountBulkUpdate{Extra: map[string]any{"codex_ticket_proxy_mode": "random"}})
	require.NoError(t, err)
	require.EqualValues(t, 2, count)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketProxyUnrelatedObservationDoesNotAddTransaction(t *testing.T) {
	r, mock := newCodexTicketCASRepo(t)
	mock.ExpectExec(`UPDATE accounts SET extra`).WithArgs(`{"codex_usage_updated_at":"observed"}`, int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	require.NoError(t, r.UpdateExtra(context.Background(), 7, map[string]any{"codex_usage_updated_at": "observed"}))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketProxyLockedFullUpdatePreservesAndValidatesConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name  string
		patch map[string]any
		mode  string
		id    int64
		valid bool
	}{
		{"omitted", map[string]any{"other": true}, "fixed", 2, true},
		{"switch to inherit", map[string]any{"codex_ticket_proxy_mode": "inherit", "codex_ticket_proxy_id": 0}, "inherit", 0, true},
		{"invalid fixed id", map[string]any{"codex_ticket_proxy_id": 0}, "", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, mock := newCodexTicketCASRepo(t)
			mock.ExpectQuery(`SELECT extra FROM accounts`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"extra"}).AddRow([]byte(`{"codex_ticket_proxy_mode":"fixed","codex_ticket_proxy_id":2}`)))
			a := &service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Extra: tc.patch}
			ctx := service.WithCodexTicketProxyWrite(context.Background(), tc.patch)
			err := preserveLockedAccountProtection(ctx, r.client, a)
			if tc.valid {
				require.NoError(t, err)
				require.Equal(t, tc.mode, a.CodexTicketProxyMode())
				require.Equal(t, tc.id, a.CodexTicketProxyID())
			} else {
				require.Error(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
