package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketRouteRotationCloneAndJSON(t *testing.T) {
	now := time.Now()
	first := usageTestTicket("A", time.Hour, now)
	second := usageTestTicket("B", 30*time.Minute, now)
	inventory := usageTestPool(first, second)
	require.True(t, codexTicketRouteRotationSetActive(inventory, first, " CHAT.GATEWAY.UNIFIED-1.API.OPENAI.COM. "))
	require.True(t, codexTicketRouteRotationCool(inventory, second, now.Add(2*time.Hour)))
	activeID, activeHost := codexTicketRouteRotationActive(inventory)
	require.Equal(t, codexTicketConsumptionID(first), activeID)
	require.Equal(t, "chat.gateway.unified-1.api.openai.com", activeHost)
	require.Nil(t, codexTicketLeaf(inventory).RouteRotation)
	require.Nil(t, codexTicketLeaf(inventory.Standby).RouteRotation)

	copy := cloneCodexTicketInventory(inventory)
	require.Equal(t, inventory.RouteRotation, copy.RouteRotation)
	delete(copy.RouteRotation.Cooldowns, codexTicketConsumptionID(second))
	require.Len(t, inventory.RouteRotation.Cooldowns, 1)

	encoded, err := json.Marshal(inventory)
	require.NoError(t, err)
	parsed := parseOpenAICodexTicketFromAny(first.AccountID, first.Model, json.RawMessage(encoded))
	require.NotNil(t, parsed)
	require.Equal(t, inventory.RouteRotation.ActiveLineageID, parsed.RouteRotation.ActiveLineageID)
	require.Equal(t, inventory.RouteRotation.ActiveHost, parsed.RouteRotation.ActiveHost)
	require.True(t, inventory.RouteRotation.UpdatedAt.Equal(parsed.RouteRotation.UpdatedAt))
	require.True(t, inventory.RouteRotation.Cooldowns[codexTicketConsumptionID(second)].Equal(
		parsed.RouteRotation.Cooldowns[codexTicketConsumptionID(second)]))
	require.Nil(t, parsed.Standby.RouteRotation)
}

func TestCodexTicketRouteRotationCooldownExpiresWithoutDeletingTicket(t *testing.T) {
	now := time.Now()
	ticket := usageTestTicket("A", time.Hour, now)
	inventory := usageTestPool(ticket)
	until := now.Add(time.Hour)
	require.True(t, codexTicketRouteRotationCool(inventory, ticket, until))
	require.Equal(t, until, codexTicketRouteRotationCooldownUntil(inventory, ticket, now))
	require.True(t, codexTicketRouteRotationCooldownUntil(inventory, ticket, until).IsZero())
	require.False(t, codexTicketRouteRotationCool(inventory, ticket, now.Add(30*time.Minute)))
	require.Equal(t, until, inventory.RouteRotation.Cooldowns[codexTicketConsumptionID(ticket)])
	require.Len(t, codexTicketSlots(inventory), 1)
}

func TestCodexTicketRouteRotationSurvivesPublicationAndPrunesRemovedTickets(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	first, second := usageTestTicket("A", 7*24*time.Hour+time.Hour, now), usageTestTicket("B", 2*time.Hour, now)
	inventory := usageTestPool(first, second)
	require.True(t, codexTicketRouteRotationSetActive(inventory, first, "old.example"))
	require.True(t, codexTicketRouteRotationCool(inventory, first, now.Add(time.Hour)))
	require.True(t, codexTicketRouteRotationCool(inventory, second, now.Add(2*time.Hour)))

	wide := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{PoolCapacity: 3})
	added := mergeCodexTicketPublication(inventory, usageTestTicket("C", 0, now), account, wide)
	require.Len(t, codexTicketSlots(added), 3)
	require.Len(t, added.RouteRotation.Cooldowns, 2)
	require.Equal(t, "old.example", added.RouteRotation.ActiveHost)

	narrow := historicalTestConfig(2)
	replaced := mergeCodexTicketPublication(inventory, usageTestTicket("D", 0, now), account, narrow)
	require.Equal(t, usageTestStates(second), usageTestStates(codexTicketSlots(replaced)[0]))
	require.Len(t, codexTicketSlots(replaced), 2)
	require.NotContains(t, replaced.RouteRotation.Cooldowns, codexTicketConsumptionID(first))
	require.Contains(t, replaced.RouteRotation.Cooldowns, codexTicketConsumptionID(second))
	require.Equal(t, "old.example", replaced.RouteRotation.ActiveHost)
}

func TestCodexTicketRouteRotationRestoresNewerPersistedStateAtEqualTicketTime(t *testing.T) {
	now := time.Now()
	ticket := usageTestTicket("A", time.Hour, now)
	svc := usageTestService(t, config.OpenAICodexTicketConfig{}, ticket)
	account := ticketTestAccount(41)
	persisted := usageTestPool(ticket)
	require.True(t, codexTicketRouteRotationSetActive(persisted, ticket, "new.example"))
	account.Extra = map[string]any{openAICodexTicketExtraKey(ticket.Model): persisted}
	got := svc.codexTicketInventoryLocked(account, ticket.Model)
	_, host := codexTicketRouteRotationActive(got)
	require.Equal(t, "new.example", host)
}

func TestCodexTicketRouteRotationStatusCountsOnlyAssignableHistoryTickets(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	cfg := historicalTestConfig(2)
	cfg.SkipSameRouteHost = true
	first := usageTestTicket("A", 7*24*time.Hour+time.Hour, now)
	second := usageTestTicket("B", 7*24*time.Hour+30*time.Minute, now)
	inventory := usageTestPool(first, second)
	until := now.Add(10 * time.Minute)
	require.True(t, codexTicketRouteRotationCool(inventory, first, until))

	status := codexTicketPoolStatus(first.Model, inventory, account, cfg, now)
	require.True(t, status.Ready)
	require.Equal(t, 1, status.AvailableCount)
	require.Equal(t, 0, status.ReserveCount)
	require.Equal(t, "route_cooldown", status.PrimaryReason)
	require.False(t, status.PrimaryReady)
	require.True(t, status.UsingStandby)

	after := codexTicketPoolStatus(first.Model, inventory, account, cfg, until)
	require.True(t, after.Ready)
	require.Equal(t, 2, after.AvailableCount)
	require.True(t, after.PrimaryReady)
	cfg.SkipSameRouteHost = false
	legacy := codexTicketPoolStatus(first.Model, inventory, account, cfg, now)
	require.Equal(t, 2, legacy.AvailableCount)
	require.True(t, legacy.PrimaryReady)
}
