package admin

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountListCodexTicketPoolSummaryNeverExposesStoredStates(t *testing.T) {
	now := time.Now()
	ticket := func(marker string, expires time.Time) map[string]any {
		return map[string]any{"state": "gAAAAA" + strings.Repeat(marker, 286), "length": 292,
			"model": "gpt-6-astra", "captured_at": now.Add(-time.Minute), "expires_at": expires}
	}
	primary := ticket("A", now.Add(time.Hour))
	primary["standby"] = ticket("B", now.Add(30*time.Second))
	primary["reserve"] = []any{ticket("C", now.Add(time.Hour))}
	account := &service.Account{ID: 41, Platform: "openai", Type: "oauth",
		Extra: map[string]any{"codex_turn_ticket:gpt-6-astra": primary}}
	handler := &AccountHandler{cfg: &config.Config{Gateway: config.GatewayConfig{
		OpenAICodexTicket: config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"gpt-6-astra"}, PoolCapacity: 5},
	}}}
	result := handler.accountListResponseFromService(account)
	require.Len(t, result.CodexTurnTickets, 1)
	status := result.CodexTurnTickets[0]
	require.Equal(t, 3, status.AvailableCount)
	require.Equal(t, 2, status.ReserveCount)
	require.Equal(t, 1, status.ExpiringCount)
	require.Equal(t, 5, status.Capacity)
	for _, output := range []any{result, dto.AccountListItemFromAccount(result)} {
		payload, err := json.Marshal(output)
		require.NoError(t, err)
		require.Contains(t, string(payload), `"available_count":3`)
		require.NotContains(t, string(payload), "gAAAAA")
		require.NotContains(t, string(payload), "codex_turn_ticket:gpt-6-astra")
	}
}
