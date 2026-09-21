package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func expectCodexTicketRevocation(mock sqlmock.Sqlmock, affected int64) {
	mock.ExpectExec(`(?s)^UPDATE accounts.*jsonb_build_object.*WHERE id = \$2 AND platform = \$3 AND type = \$4.*extra -> \$1 ->> 'state'\) = \$5.*extra -> \$1 ->> 'captured_at'\) = \$6.*deleted_at IS NULL$`).
		WithArgs("codex_turn_ticket:model", int64(41), service.PlatformOpenAI, service.AccountTypeOAuth, "old", "2026-09-20T01:00:00Z").
		WillReturnResult(sqlmock.NewResult(0, affected))
}

func TestCompareAndSwapCodexTicketRevocationUsesTicketIdentityAfterAccountRefresh(t *testing.T) {
	for _, affected := range []int64{0, 1} {
		repo, mock := newCodexTicketCASRepo(t)
		account := codexTicketCASAccount()
		account.Credentials["access_token"] = "stale-token"
		account.Status = service.StatusDisabled
		// 账号代理配置已变更甚至被删除，也不能阻止旧响应撤销它实际使用的票。
		setCodexTicketCASProxy(account)
		account.Proxy = nil
		account.Extra["codex_turn_ticket:model"] = map[string]any{"state": "old", "captured_at": "2026-09-20T01:00:00Z"}
		expectCodexTicketRevocation(mock, affected)
		changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model",
			map[string]any{"state": "old", "captured_at": "2026-09-20T01:00:00Z", "revoked": true})
		require.NoError(t, err)
		require.Equal(t, affected == 1, changed, "身份不同的新票必须由SQL条件拒绝")
		require.NoError(t, mock.ExpectationsWereMet())
	}
}

func TestCompareAndSwapCodexTicketRevocationRejectsUnprovenIdentity(t *testing.T) {
	for _, replacement := range []any{
		map[string]any{"state": "new", "captured_at": "2026-09-20T01:00:00Z", "revoked": true},
		map[string]any{"state": "old", "captured_at": "2026-09-20T01:00:01Z", "revoked": true},
		map[string]any{"state": "old", "revoked": true},
	} {
		repo, mock := newCodexTicketCASRepo(t)
		account := codexTicketCASAccount()
		account.Extra["codex_turn_ticket:model"] = map[string]any{"state": "old", "captured_at": "2026-09-20T01:00:00Z"}
		changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", replacement)
		require.Error(t, err)
		require.False(t, changed)
		require.NoError(t, mock.ExpectationsWereMet(), "没有对应发送快照的撤销不应进入SQL")
	}
}

func TestCompareAndSwapCodexTicketRevocationIsIdempotent(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	account := codexTicketCASAccount()
	account.Extra["codex_turn_ticket:model"] = map[string]any{"state": "old", "captured_at": "2026-09-20T01:00:00Z", "revoked": true}
	for range 2 {
		expectCodexTicketRevocation(mock, 1)
		changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", account.Extra["codex_turn_ticket:model"])
		require.NoError(t, err)
		require.True(t, changed)
	}
	require.NoError(t, mock.ExpectationsWereMet())
}
