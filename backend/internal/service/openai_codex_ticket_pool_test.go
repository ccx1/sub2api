package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketPoolReusesPrimaryAndFallsThroughEveryStoredTicket(t *testing.T) {
	s, account, primary, _ := ticketWatchdogFixture(t)
	s.cfg.Gateway.OpenAICodexTicket.PoolCapacity = 5
	tickets := []*openAICodexTicket{primary}
	for index, marker := range []string{"C", "D", "E", "F"} {
		ticket := inventoryTestTicket(primary, marker, time.Duration(index+1)*time.Second)
		require.True(t, s.storeOpenAICodexTicket(context.Background(), account, ticket))
		tickets = append(tickets, ticket)
	}
	stored, _ := s.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, primary.Model))
	payload, err := json.Marshal(stored)
	require.NoError(t, err)
	var persisted map[string]any
	require.NoError(t, json.Unmarshal(payload, &persisted))
	account.Extra = map[string]any{openAICodexTicketExtraKey(primary.Model): persisted}
	cold := ticketTestService(t, s.openAICodexTicketConfig(), nil)
	for index, expected := range tickets {
		for range 2 {
			req, err := http.NewRequest(http.MethodPost, "http://localhost", nil)
			require.NoError(t, err)
			require.NoError(t, cold.applyOpenAICodexTicketRequest(account, primary.Model, req))
			require.Equal(t, expected.State, req.Header.Get(openAICodexTurnStateHeader))
			receipt := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
			require.Empty(t, receipt.ticket.Reserve)
			require.Nil(t, receipt.ticket.Standby)
		}
		cold.invalidateOpenAICodexTicket(context.Background(), account, expected)
		if index+1 < len(tickets) {
			require.Equal(t, tickets[index+1].State, cold.lookupOpenAICodexTicket(account, primary.Model).State)
		}
	}
	require.ErrorIs(t, cold.applyOpenAICodexTicket(context.Background(), account, primary.Model, http.Header{}), ErrOpenAICodexTicketUnavailable)
}

func TestCodexTicketPoolRefillsToCapacityAndKeepsCurrentUntilInvalid(t *testing.T) {
	s, account, primary, _ := ticketWatchdogFixture(t)
	s.cfg.Gateway.OpenAICodexTicket.PoolCapacity = 3
	first := inventoryTestTicket(primary, "C", time.Second)
	first.ExpiresAt = time.Now().Add(30 * time.Second)
	second := inventoryTestTicket(primary, "D", 2*time.Second)
	for _, ticket := range []*openAICodexTicket{first, second} {
		require.True(t, s.storeOpenAICodexTicket(context.Background(), account, ticket))
	}
	cfg := s.openAICodexTicketConfig()
	require.True(t, s.codexTicketInventoryNeedsRefresh(account, primary.Model, cfg, time.Now()))
	fresh := inventoryTestTicket(primary, "E", 3*time.Second)
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, fresh))
	require.False(t, s.codexTicketInventoryNeedsRefresh(account, primary.Model, cfg, time.Now()))
	stored, _ := s.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, primary.Model))
	inventory := stored.(*openAICodexTicket)
	require.Equal(t, primary.State, s.lookupOpenAICodexTicket(account, primary.Model).State)
	require.False(t, codexTicketInventoryContains(inventory, first))
	require.True(t, codexTicketInventoryContains(inventory, second))
	require.True(t, codexTicketInventoryContains(inventory, fresh))
	// 重复收票不能扩充库存，也不能延长原票有效期。
	duplicate := *fresh
	duplicate.CapturedAt = duplicate.CapturedAt.Add(time.Second)
	duplicate.ExpiresAt = duplicate.ExpiresAt.Add(time.Hour)
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, &duplicate))
	stored, _ = s.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, primary.Model))
	require.Equal(t, 3, len(codexTicketSlots(stored.(*openAICodexTicket))))
}

func TestCodexTicketPoolStatusCountsOnlyDistinctUsableTickets(t *testing.T) {
	_, account, primary, _ := ticketWatchdogFixture(t)
	soon := inventoryTestTicket(primary, "C", time.Second)
	soon.ExpiresAt = time.Now().Add(30 * time.Second)
	fresh := inventoryTestTicket(primary, "D", 2*time.Second)
	revoked := inventoryTestTicket(primary, "E", 3*time.Second)
	revoked.Revoked = true
	expired := inventoryTestTicket(primary, "F", 4*time.Second)
	expired.ExpiresAt = time.Now().Add(-time.Second)
	inventory := *primary
	inventory.Standby, inventory.Reserve = soon, []*openAICodexTicket{fresh, revoked, expired, primary}
	account.Extra = map[string]any{openAICodexTicketExtraKey(primary.Model): &inventory}
	cfg := config.OpenAICodexTicketConfig{Enabled: true, PoolCapacity: 5, Models: []string{primary.Model}}
	status := OpenAICodexTicketStatuses(account, cfg, time.Now())[0]
	require.Equal(t, 3, status.AvailableCount)
	require.Equal(t, 5, status.Capacity)
	require.Equal(t, 2, status.ReserveCount)
	require.Equal(t, 1, status.ExpiringCount)
	require.True(t, status.NextExpiresAt.Equal(soon.ExpiresAt))
	require.True(t, status.PrimaryPresent)
	require.True(t, status.PrimaryReady)
	encoded, err := json.Marshal(status)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), primary.State)
}

func TestCodexTicketPoolStatusShowsUnavailablePrimaryReason(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	account.Extra = map[string]any{openAICodexTicketExtraKey("gpt-6-astra"): map[string]any{
		"model":       "gpt-6-astra",
		"state":       fakeCodexTicketState(292),
		"length":      292,
		"verified":    true,
		"expires_at":  now.Add(time.Minute),
		"revoked":     true,
		"captured_at": now.Add(-time.Minute),
	}}
	status := OpenAICodexTicketStatuses(account, config.OpenAICodexTicketConfig{
		Enabled: true, FailClosed: true, Models: []string{"gpt-6-astra"},
	}, now)[0]
	require.True(t, status.PrimaryPresent)
	require.False(t, status.PrimaryReady)
	require.Equal(t, "revoked", status.PrimaryReason)
	require.Equal(t, 0, status.AvailableCount)
}

func TestCodexTicketPoolCapacitySettingsRoundTripAndValidation(t *testing.T) {
	s, repo := newTicketPolicySettings()
	ctx := context.Background()
	cfg, err := s.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 5, cfg.PoolCapacity)
	for _, capacity := range []int{1, 5, 20} {
		cfg.PoolCapacity = capacity
		_, err = s.UpdateCodexTicketSettings(ctx, cfg)
		require.NoError(t, err)
		restarted := NewSettingService(repo, s.cfg)
		reloaded, err := restarted.GetCodexTicketSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, capacity, reloaded.PoolCapacity)
	}
	for _, capacity := range []int{-1, 21} {
		cfg.PoolCapacity = capacity
		_, err = s.UpdateCodexTicketSettings(ctx, cfg)
		require.Error(t, err)
	}
	// 老安装没有容量字段时按默认容量读取。
	cfg.PoolCapacity = 0
	_, err = s.UpdateCodexTicketSettings(ctx, cfg)
	require.NoError(t, err)
	reloaded, err := s.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 5, reloaded.PoolCapacity)
}

func TestCodexTicketPoolDefaultHarvestFillsFiveThenStops(t *testing.T) {
	account := ticketTestAccount(41)
	account.Status = StatusActive
	upstream := &codexTicketVerificationUpstream{respond: func(call int) *http.Response {
		marker := string(rune('C' + (call-1)/2))
		return codexTicketCompletedResponse("gpt-6-astra", inventoryTestTicket(&openAICodexTicket{}, marker, 0).State)
	}}
	cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{
		Enabled: true, Models: []string{"gpt-6-astra"}, HarvestProxyURL: "http://harvest.example:8080",
	})
	s := ticketTestService(t, cfg, upstream)
	s.accountRepo = &codexTicketRefreshRepo{accounts: []Account{*account}}
	for range 7 {
		s.refreshOpenAICodexTickets(context.Background())
	}
	require.Len(t, upstream.requests, 10, "默认五张，每张采集加复验，补满后停止")
	stored, _ := s.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, "gpt-6-astra"))
	require.Len(t, codexTicketSlots(stored.(*openAICodexTicket)), 5)
	other := ticketTestAccount(42)
	require.Nil(t, s.lookupOpenAICodexTicket(other, "gpt-6-astra"))
	require.Nil(t, s.lookupOpenAICodexTicket(account, "other-model"))
}

func TestCodexTicketPoolCapacityOneAndReductionKeepUsableInventory(t *testing.T) {
	s, account, primary, _ := ticketWatchdogFixture(t)
	s.cfg.Gateway.OpenAICodexTicket.PoolCapacity = 1
	cfg := s.openAICodexTicketConfig()
	require.False(t, s.codexTicketInventoryNeedsRefresh(account, primary.Model, cfg, time.Now()))
	primary.ExpiresAt = time.Now().Add(time.Second)
	s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, primary.Model), primary)
	require.True(t, s.codexTicketInventoryNeedsRefresh(account, primary.Model, cfg, time.Now()))
	fresh := inventoryTestTicket(primary, "C", time.Second)
	fresh.ExpiresAt = time.Now().Add(time.Hour)
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, fresh))
	require.Equal(t, fresh.State, s.lookupOpenAICodexTicket(account, primary.Model).State)
	require.False(t, s.codexTicketInventoryNeedsRefresh(account, primary.Model, cfg, time.Now()))
	s.cfg.Gateway.OpenAICodexTicket.PoolCapacity = 5
	for index, marker := range []string{"D", "E", "F", "G"} {
		require.True(t, s.storeOpenAICodexTicket(context.Background(), account, inventoryTestTicket(fresh, marker, time.Duration(index+1)*time.Second)))
	}
	s.cfg.Gateway.OpenAICodexTicket.PoolCapacity = 2
	require.True(t, s.storeOpenAICodexTicket(context.Background(), account, inventoryTestTicket(fresh, "H", 5*time.Second)))
	stored, _ := s.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, primary.Model))
	require.Len(t, codexTicketSlots(stored.(*openAICodexTicket)), 2)
	require.Equal(t, fresh.State, s.lookupOpenAICodexTicket(account, primary.Model).State)
}
