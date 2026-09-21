package admin

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountResponseCodexTicketsUsesPersistedTierPolicy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Models: []string{"legacy-model"}, TargetLength: 292}
	repo := &settingHandlerRepoStub{}
	settings := service.NewSettingService(repo, cfg)
	policy := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{
		Enabled: true, FailClosed: true, Models: []string{"gpt-6-astra"},
	})
	_, err := settings.UpdateCodexTicketSettings(context.Background(), policy)
	require.NoError(t, err)
	settings = service.NewSettingService(repo, cfg)
	h := &AccountHandler{cfg: cfg}
	h.SetCodexTicketSettings(settings)
	account := &service.Account{ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"plan_type": "team"}, Extra: map[string]any{
			"codex_turn_ticket:gpt-6-astra": map[string]any{"model": "gpt-6-astra", "state": "gAAAAA" + strings.Repeat("A", 326),
				"length": 332, "expires_at": time.Now().Add(time.Hour)},
		}}
	for _, view := range []bool{false, true} {
		out := h.accountResponseFromService(account)
		if view {
			out = h.accountListResponseFromService(account)
		}
		require.True(t, *out.CodexTicketGlobalEnabled)
		require.Len(t, out.CodexTurnTickets, 1)
		require.Equal(t, "gpt-6-astra", out.CodexTurnTickets[0].Model)
		require.True(t, out.CodexTurnTickets[0].Ready)
	}
	policy.TierRules[0].TargetLength = 352
	_, err = settings.UpdateCodexTicketSettings(context.Background(), policy)
	require.NoError(t, err)
	status := h.accountListResponseFromService(account).CodexTurnTickets[0]
	require.False(t, status.Ready)
	require.True(t, status.Blocked)
	policy.Enabled = false
	_, err = settings.UpdateCodexTicketSettings(context.Background(), policy)
	require.NoError(t, err)
	out := h.accountResponseFromService(account)
	require.False(t, *out.CodexTicketGlobalEnabled)
	require.Empty(t, out.CodexTurnTickets)
	require.False(t, cfg.Gateway.OpenAICodexTicket.Enabled)
	require.Equal(t, []string{"legacy-model"}, cfg.Gateway.OpenAICodexTicket.Models)
}
