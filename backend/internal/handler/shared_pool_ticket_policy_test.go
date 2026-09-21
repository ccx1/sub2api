package handler

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type sharedTicketPolicyRepository struct {
	service.SettingRepository
	values map[string]string
}

func (r *sharedTicketPolicyRepository) GetValue(_ context.Context, key string) (string, error) {
	value, exists := r.values[key]
	if !exists {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (r *sharedTicketPolicyRepository) SetMultiple(_ context.Context, values map[string]string) error {
	if r.values == nil {
		r.values = make(map[string]string)
	}
	for key, value := range values {
		r.values[key] = value
	}
	return nil
}

func TestSharedTicketViewsUseProductionRuntimeSettings(t *testing.T) {
	cfg := &config.Config{}
	repo := &sharedTicketPolicyRepository{}
	settings := service.NewSettingService(repo, cfg)
	policy := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{
		Enabled: true, FailClosed: true, Models: []string{"gpt-6-astra"},
	})
	_, err := settings.UpdateCodexTicketSettings(context.Background(), policy)
	require.NoError(t, err)
	settings = service.NewSettingService(repo, cfg)
	h := NewSharedPoolHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, cfg, settings)
	now := time.Now()
	account := &service.Account{ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive,
		Credentials: map[string]any{"plan_type": "team"}, Extra: map[string]any{
			"codex_turn_ticket:gpt-6-astra": map[string]any{"model": "gpt-6-astra", "state": "gAAAAA" + strings.Repeat("A", 326),
				"length": 332, "expires_at": now.Add(time.Hour)},
		}}
	snapshot := service.NewSharedPoolTicketAccountSnapshot(account, now)
	require.NotNil(t, snapshot)
	snapshot.Available, snapshot.Concurrency = true, 2
	capacity := &service.SharedPoolCapacity{AvailableAccounts: 1, ConcurrencyCapacity: 2,
		TicketAccounts: []service.SharedPoolTicketAccountSnapshot{*snapshot}}
	view := sharedAccountUsageView{}
	h.enrichSharedUsageView(context.Background(), &view, account)
	require.Len(t, view.CodexTurnTickets, 1)
	require.True(t, view.CodexTurnTickets[0].Ready)
	require.Equal(t, int64(2), service.GetSharedPoolCatalogCapacity(capacity, h.sharedTicketConfig(context.Background()), now).ConcurrencyCapacity)
	policy.TierRules[0].TargetLength = 352
	_, err = settings.UpdateCodexTicketSettings(context.Background(), policy)
	require.NoError(t, err)
	h.enrichSharedUsageView(context.Background(), &view, account)
	require.False(t, view.CodexTurnTickets[0].Ready)
	require.True(t, view.CodexTurnTickets[0].Blocked)
	require.Zero(t, service.GetSharedPoolCatalogCapacity(capacity, h.sharedTicketConfig(context.Background()), now).AvailableAccounts)
	policy.Enabled = false
	_, err = settings.UpdateCodexTicketSettings(context.Background(), policy)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.enrichSharedUsageView(ctx, &view, account)
	require.Empty(t, view.CodexTurnTickets)
	require.Equal(t, int64(2), service.GetSharedPoolCatalogCapacity(capacity, h.sharedTicketConfig(ctx), now).ConcurrencyCapacity)
	require.False(t, cfg.Gateway.OpenAICodexTicket.Enabled)
}
