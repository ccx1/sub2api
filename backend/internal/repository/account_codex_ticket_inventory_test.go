package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketRevocationRecordsStandbyInvalidation(t *testing.T) {
	for _, alreadyRevoked := range []bool{false, true} {
		repo, mock := newCodexTicketCASRepo(t)
		account, replacement, event := codexTicketInvalidationFixture()
		standby := account.Extra["codex_turn_ticket:model"].(map[string]any)
		standby["revoked"] = alreadyRevoked
		account.Extra["codex_turn_ticket:model"] = map[string]any{
			"state": "primary", "captured_at": "2026-09-20T00:00:00Z",
			"attempt_id": "primary-attempt", "model": "model", "standby": standby,
		}
		payload, err := json.Marshal(event)
		require.NoError(t, err)
		expectCodexTicketInvalidation(mock).WithArgs("codex_turn_ticket:model", int64(41), service.PlatformOpenAI,
			service.AccountTypeOAuth, "private-ticket", "2026-09-20T01:00:00Z", codexTicketCASJSON(payload),
			service.OpenAICodexTicketInvalidationsKey, service.OpenAICodexTicketInvalidationsLimit).
			WillReturnResult(sqlmock.NewResult(0, 1))
		changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", replacement)
		require.NoError(t, err)
		require.True(t, changed)
		require.NotContains(t, account.Extra, service.OpenAICodexTicketInvalidationsKey)
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestCodexTicketRevocationFindsSentStandbyAfterCredentialChange(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	account := codexTicketCASAccount()
	account.Status = service.StatusDisabled
	account.Extra["codex_turn_ticket:model"] = map[string]any{
		"state": "primary", "captured_at": "2026-09-20T01:00:00Z",
		"standby": map[string]any{"state": "backup", "captured_at": "2026-09-20T02:00:00Z"},
	}
	mock.ExpectExec(`(?s)^UPDATE accounts.*standby.*WHERE id = \$2 AND platform = \$3 AND type = \$4.*standby.*deleted_at IS NULL$`).
		WithArgs("codex_turn_ticket:model", int64(41), service.PlatformOpenAI, service.AccountTypeOAuth, "backup", "2026-09-20T02:00:00Z").
		WillReturnResult(sqlmock.NewResult(0, 1))
	changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", map[string]any{
		"state": "backup", "captured_at": "2026-09-20T02:00:00Z", "revoked": true,
	})
	require.NoError(t, err)
	require.True(t, changed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexTicketRevocationDistinguishesSameStateDifferentCaptures(t *testing.T) {
	account := codexTicketCASAccount()
	account.Extra["codex_turn_ticket:model"] = map[string]any{
		"state": "same-state", "captured_at": "2026-09-20T01:00:00Z",
		"standby": map[string]any{"state": "same-state", "captured_at": "2026-09-20T02:00:00Z"},
	}
	for _, captured := range []string{"2026-09-20T01:00:00Z", "2026-09-20T02:00:00Z"} {
		request, err := prepareCodexTicketCAS(account, "model", map[string]any{
			"state": "same-state", "captured_at": captured, "revoked": true,
		})
		require.NoError(t, err)
		require.True(t, request.revoke)
		require.Equal(t, captured, request.args[5])
	}
	_, err := prepareCodexTicketCAS(account, "model", map[string]any{
		"state": "same-state", "captured_at": "2026-09-20T03:00:00Z", "revoked": true,
	})
	require.Error(t, err, "迟到回调不能撤销不同采集版本")
}
