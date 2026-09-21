//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedImportCodexTicketOnlyAppliesToCompatibleAccounts(t *testing.T) {
	content := `[
		{"name":"openai","platform":"openai","type":"oauth","credentials":{"access_token":"openai-token"},"codex_ticket_enabled":true,"extra":{"codex_ticket_enabled":true,"codex_turn_ticket:model":{"state":"spoofed"}}},
		{"name":"gemini","platform":"gemini","type":"oauth","credentials":{"access_token":"gemini-token"}},
		{"name":"api-key","platform":"openai","type":"apikey","credentials":{"api_key":"api-token"}},
		{"tokens":{"access_token":"codex-token"}}
	]`
	for _, raw := range []string{`{}`, `{"codex_ticket_enabled":false}`, `{"codex_ticket_enabled":true}`} {
		t.Run(raw, func(t *testing.T) {
			defaults := importTestDefaults()
			defaults.Enabled, defaults.ProtectionEnabled = false, false
			require.NoError(t, json.Unmarshal([]byte(raw), &defaults))
			entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: content}}, Defaults: defaults})
			require.NoError(t, err)
			require.Len(t, entries, 4)
			for _, index := range []int{0, 3} {
				require.Equal(t, defaults.CodexTicketEnabled, entries[index].input.CodexTicketEnabled)
			}
			for _, index := range []int{1, 2} {
				require.Nil(t, entries[index].input.CodexTicketEnabled)
			}
			r := &sharedImportRepositoryStub{accounts: map[int64]*service.Account{}, owners: map[int64]int64{}, seen: map[string]bool{}}
			pool := service.NewSharedPoolService(r, r, nil, nil, nil, sharedImportEarningsStub{}, nil)
			result, err := executeSharedImport(context.Background(), 901, entries, pool.Create)
			require.NoError(t, err)
			require.Equal(t, 4, result.Created)
			require.Zero(t, result.Failed)
			for _, account := range r.accounts {
				require.NotContains(t, account.Extra, "codex_turn_ticket:model")
				if account.IsOpenAIOAuthLike() {
					want := defaults.CodexTicketEnabled == nil || *defaults.CodexTicketEnabled
					require.Equal(t, want, service.OpenAICodexTicketAccountEnabled(account))
				} else {
					require.NotContains(t, account.Extra, service.OpenAICodexTicketEnabledExtraKey)
				}
			}
		})
	}
}
