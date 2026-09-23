package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func codexTicketPoolFixture() (*service.Account, []map[string]any) {
	account := codexTicketCASAccount()
	tickets := make([]map[string]any, 5)
	for index := range tickets {
		tickets[index] = map[string]any{"state": fmt.Sprintf("ticket-%d", index),
			"captured_at": fmt.Sprintf("2026-09-20T0%d:00:00Z", index), "model": "model",
			"attempt_id": fmt.Sprintf("attempt-%d", index)}
	}
	root := make(map[string]any, len(tickets[0])+2)
	for key, value := range tickets[0] {
		root[key] = value
	}
	root["standby"], root["reserve"] = tickets[1], tickets[2:]
	account.Extra["codex_turn_ticket:model"] = root
	return account, tickets
}

func TestCodexTicketPoolRevokesEverySlot(t *testing.T) {
	for index := range 5 {
		t.Run(fmt.Sprintf("slot-%d", index), func(t *testing.T) {
			repo, mock := newCodexTicketCASRepo(t)
			account, tickets := codexTicketPoolFixture()
			used := tickets[index]
			replacement := map[string]any{"state": used["state"], "captured_at": used["captured_at"], "revoked": true}
			before, err := json.Marshal(account.Extra)
			require.NoError(t, err)
			mock.ExpectExec(regexp.QuoteMeta(codexTicketRevocationSQL)).WithArgs("codex_turn_ticket:model",
				int64(41), service.PlatformOpenAI, service.AccountTypeOAuth, used["state"], used["captured_at"]).
				WillReturnResult(sqlmock.NewResult(0, 1))
			changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", replacement)
			require.NoError(t, err)
			require.True(t, changed)
			after, err := json.Marshal(account.Extra)
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after), "撤票不能修改调用方的快照")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCodexTicketPoolRevocationRequiresExactCapture(t *testing.T) {
	account, tickets := codexTicketPoolFixture()
	for _, ticket := range tickets {
		ticket["state"] = "same-state"
	}
	for _, captured := range []string{"2026-09-20T02:00:00Z", "2026-09-20T03:00:00Z", "2026-09-20T04:00:00Z"} {
		request, err := prepareCodexTicketCAS(account, "model", map[string]any{
			"state": "same-state", "captured_at": captured, "revoked": true,
		})
		require.NoError(t, err)
		require.Equal(t, captured, request.args[5])
	}
	for _, captured := range []string{"2026-09-20T05:00:00Z", "", "invalid"} {
		_, err := prepareCodexTicketCAS(account, "model", map[string]any{
			"state": "same-state", "captured_at": captured, "revoked": true,
		})
		require.Error(t, err)
	}
}

func TestCodexTicketPoolInvalidationUsesReserveMetadata(t *testing.T) {
	account, replacement, event := codexTicketInvalidationFixture()
	used := account.Extra["codex_turn_ticket:model"]
	account, _ = codexTicketPoolFixture()
	account.Extra["codex_turn_ticket:model"].(map[string]any)["reserve"] = []any{used}
	request, err := prepareCodexTicketCAS(account, "model", replacement)
	require.NoError(t, err)
	require.True(t, request.withInvalidation)
	var got service.CodexTicketInvalidation
	require.NoError(t, json.Unmarshal([]byte(request.args[6].(string)), &got))
	require.Equal(t, event, got)
	require.NotContains(t, request.args[6], "private-ticket")
}

func TestCodexTicketPoolPublicationComparesWholeInventory(t *testing.T) {
	account, _ := codexTicketPoolFixture()
	request, err := prepareCodexTicketCAS(account, "model", map[string]any{"state": "next"})
	require.NoError(t, err)
	expected, err := json.Marshal(account.Extra["codex_turn_ticket:model"])
	require.NoError(t, err)
	require.JSONEq(t, string(expected), request.args[7].(string))
}
