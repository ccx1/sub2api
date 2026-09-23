package repository

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketCredentialPartialUpdatesValidateAndClearUnderLock(t *testing.T) {
	for _, test := range []struct {
		name   string
		policy any
		valid  bool
	}{
		{"save", map[string]any{"mode": "cookie", "ttl_seconds": 12, "refresh_before_seconds": 0}, true},
		{"clear", nil, true},
		{"inherit", map[string]any{"mode": "inherit"}, true},
		{"invalid", map[string]any{"mode": "cookie", "ttl_seconds": 1, "refresh_before_seconds": 1}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r, mock := newCodexTicketCASRepo(t)
			patch := map[string]any{service.CodexTicketCredentialPolicyExtraKey: test.policy}
			mock.ExpectBegin()
			expectTicketProxyLock(mock, "{7}", `{"codex_ticket_credential_policy":{"mode":"state"}}`)
			if test.valid {
				payload, err := json.Marshal(patch)
				require.NoError(t, err)
				mock.ExpectExec(`UPDATE accounts SET extra`).WithArgs(string(payload), int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			err := r.UpdateExtra(context.Background(), 7, patch)
			if test.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCodexTicketCredentialFullUpdatePreservesLatestUnlessExplicit(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		r, mock := newCodexTicketCASRepo(t)
		mock.ExpectBegin()
		expectTicketProxyFullUpdateLocks(mock, `{"codex_ticket_credential_policy":{"mode":"cookie","ttl_seconds":15}}`)
		mock.ExpectExec(`(?s)UPDATE .*accounts.*SET.*WHERE .*id.*`).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectQuery(`(?s)SELECT .* FROM "accounts" WHERE "id" = \$1`).WithArgs(int64(27)).WillReturnRows(updatedAccountRows(27, `{}`))
		mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		account := &service.Account{ID: 27, Name: "synced", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
			Credentials: map[string]any{"access_token": "test"}, Extra: map[string]any{service.CodexTicketCredentialPolicyExtraKey: map[string]any{"mode": "state"}},
			Concurrency: 1, Priority: 1, Status: service.StatusActive, Schedulable: true}
		ctx := context.Background()
		want := "cookie"
		if explicit {
			ctx = service.WithCodexTicketProxyWrite(ctx, account.Extra)
			want = "state"
		}
		require.NoError(t, r.Update(ctx, account))
		require.Equal(t, want, account.Extra[service.CodexTicketCredentialPolicyExtraKey].(map[string]any)["mode"])
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestCodexTicketCredentialCASAndSQLPreservation(t *testing.T) {
	before := codexTicketCASConfig(map[string]any{service.CodexTicketCredentialPolicyExtraKey: map[string]any{"mode": "state"}})
	after := codexTicketCASConfig(map[string]any{service.CodexTicketCredentialPolicyExtraKey: map[string]any{"mode": "cookie"}})
	require.NotEqual(t, before, after)
	require.Contains(t, preserveCodexTicketProxyExtraSQL("$1::jsonb"), service.CodexTicketCredentialPolicyExtraKey)
	require.True(t, needsCodexTicketProxyTransaction(context.Background(), map[string]any{service.CodexTicketCredentialPolicyExtraKey: nil}))
	policy := map[string]any{"mode": "cookie", "ttl_seconds": 15}
	require.Equal(t, policy, filterSchedulerExtra(map[string]any{service.CodexTicketCredentialPolicyExtraKey: policy})[service.CodexTicketCredentialPolicyExtraKey])
}

func TestCodexTicketCookieOnlyRevocationFindsSentVersion(t *testing.T) {
	account := codexTicketCASAccount()
	cookie := []*http.Cookie{{Name: "ticket", Value: "private-cookie"}}
	account.Extra["codex_turn_ticket:model"] = map[string]any{
		"state": "", "credential_mode": "cookie", "cookies": cookie, "captured_at": "2026-09-20T01:00:00Z",
		"standby": map[string]any{"state": "", "credential_mode": "cookie", "cookies": cookie, "captured_at": "2026-09-20T02:00:00Z"},
	}
	for _, captured := range []string{"2026-09-20T01:00:00Z", "2026-09-20T02:00:00Z"} {
		request, err := prepareCodexTicketCAS(account, "model", map[string]any{
			"state": "", "credential_mode": "cookie", "cookies": cookie, "captured_at": captured, "revoked": true,
		})
		require.NoError(t, err)
		require.True(t, request.revoke)
		require.Empty(t, request.args[4])
		require.Equal(t, captured, request.args[5])
	}
	for _, ticket := range []map[string]any{
		{"state": "", "cookies": cookie},
		{"state": "", "credential_mode": "cookie_state", "cookies": cookie},
		{"state": "", "credential_mode": "cookie"},
		{"state": "", "credential_mode": "cookie", "cookies": []*http.Cookie{nil}},
		{"state": "", "credential_mode": "cookie", "cookies": cookie, "captured_at": "2026-09-20T03:00:00Z"},
	} {
		ticket["revoked"] = true
		_, err := prepareCodexTicketCAS(account, "model", ticket)
		require.Error(t, err)
	}
}
