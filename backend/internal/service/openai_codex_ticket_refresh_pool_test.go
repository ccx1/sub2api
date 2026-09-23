package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketRefreshReplaceUpdatesEveryDueSlotAndStops(t *testing.T) {
	svc, account, primary := codexRefreshStrategyFixture(t, "")
	svc.cfg.Gateway.OpenAICodexTicket.PoolCapacity = 3
	standby, reserve := codexTicketLeaf(primary), codexTicketLeaf(primary)
	standby.State = codexTicketStateForExpiryTest(time.Now().Add(-6*time.Minute), 10)
	reserve.State = codexTicketStateForExpiryTest(time.Now().Add(-7*time.Minute), 10)
	reserve.RevalidateAt = time.Now().Add(3 * time.Minute)
	primary.Standby, primary.Reserve = standby, []*openAICodexTicket{reserve}
	saveCodexRefreshInventory(svc, account, primary)
	var states []string
	fresh := []string{codexTicketStateForExpiryTest(time.Now().Add(-20*time.Minute), 10),
		codexTicketStateForExpiryTest(time.Now().Add(-21*time.Minute), 10)}
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		states = append(states, req.Header.Get(openAICodexTurnStateHeader))
		return codexTicketCompletedResponse(primary.Model, fresh[min((len(states)-1)/2, 1)]), nil
	}}
	for range 4 {
		svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, primary.Model)
	}
	require.Equal(t, []string{"", fresh[0], "", fresh[1]}, states)
	raw, ok := svc.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, primary.Model))
	require.True(t, ok)
	inventory := raw.(*openAICodexTicket)
	require.Equal(t, fresh[0], inventory.State)
	require.Equal(t, fresh[1], inventory.Standby.State)
	require.Len(t, inventory.Reserve, 1)
	require.True(t, sameCodexTicket(reserve, inventory.Reserve[0]), "未到窗口的备用票保持不变")
	require.False(t, svc.codexTicketInventoryNeedsRefresh(account, primary.Model, svc.openAICodexTicketConfig(), time.Now()))
}

func TestCodexTicketRefreshReplaceSeesPrimaryWindowInFullPool(t *testing.T) {
	svc, account, primary := codexRefreshStrategyFixture(t, "")
	svc.cfg.Gateway.OpenAICodexTicket.PoolCapacity = 2
	standby := codexTicketLeaf(primary)
	standby.State = codexTicketStateForExpiryTest(time.Now().Add(-6*time.Minute), 10)
	standby.RevalidateAt = time.Now().Add(3 * time.Minute)
	primary.Standby = standby
	saveCodexRefreshInventory(svc, account, primary)
	require.True(t, svc.codexTicketInventoryNeedsRefresh(account, primary.Model, svc.openAICodexTicketConfig(), time.Now()),
		"满池主票进入提前刷新窗口也必须更新")
}

func TestCodexTicketRefreshReplaceFillsMissingSlotsAndStops(t *testing.T) {
	svc, account, primary := codexRefreshStrategyFixture(t, "")
	svc.cfg.Gateway.OpenAICodexTicket.PoolCapacity = 3
	primary.RevalidateAt = time.Now().Add(3 * time.Minute)
	saveCodexRefreshInventory(svc, account, primary)
	calls := 0
	now := time.Now()
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls++
		fresh := codexTicketStateForExpiryTest(now.Add(-time.Duration(20+(calls-1)/2)*time.Minute), 10)
		return codexTicketCompletedResponse(primary.Model, fresh), nil
	}}
	for range 4 {
		svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, primary.Model)
	}
	require.Equal(t, 4, calls)
	raw, ok := svc.openaiCodexTickets.Load(openAICodexTicketKey(account.ID, primary.Model))
	require.True(t, ok)
	inventory := raw.(*openAICodexTicket)
	require.Len(t, codexTicketSlots(inventory), 3)
	require.True(t, sameCodexTicket(primary, inventory))
}
