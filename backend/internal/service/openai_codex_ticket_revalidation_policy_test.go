package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketRevalidationKeepsValidOldTicketNearHardExpiry(t *testing.T) {
	svc, account, old := codexRevalidationFixture(t, "")
	svc.cfg.Gateway.OpenAICodexTicket.TTLSeconds = 3600
	svc.cfg.Gateway.OpenAICodexTicket.RefreshBeforeSeconds = 600
	now := time.Now()
	old.State = codexTicketStateForExpiryTest(now.Add(-58*time.Minute), 10)
	old.Length = len(old.State)
	hydrateCodexTicketStateExpiry(old)
	old.RevalidateAt = old.StateExpiresAt
	require.True(t, old.StateExpiresAt.After(now))
	svc.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, old.Model), cloneCodexTicketInventory(old))
	account.Extra[openAICodexTicketExtraKey(old.Model)] = cloneCodexTicketInventory(old)
	calls := 0
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, old.State, req.Header.Get(openAICodexTurnStateHeader), "valid old credentials must be rechecked before minting")
		return codexTicketCompletedResponse(old.Model, ""), nil
	}}
	for range 3 {
		svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
	}
	next := svc.lookupOpenAICodexTicket(account, old.Model)
	require.Equal(t, 3, calls)
	require.Equal(t, old.State, next.State)
	require.Equal(t, old.StateExpiresAt, next.StateExpiresAt)
	require.Nil(t, next.Standby)
}

func TestCodexTicketRevalidationMintsAfterKnownHardExpiry(t *testing.T) {
	svc, account, old := codexRevalidationFixture(t, "")
	old.State = codexTicketStateForExpiryTest(time.Now().Add(-2*time.Hour), 10)
	old.Length = len(old.State)
	hydrateCodexTicketStateExpiry(old)
	old.RevalidateAt = old.StateExpiresAt
	svc.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, old.Model), cloneCodexTicketInventory(old))
	account.Extra[openAICodexTicketExtraKey(old.Model)] = cloneCodexTicketInventory(old)
	fresh := codexTicketStateForExpiryTest(time.Now(), 10)
	var states []string
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		states = append(states, req.Header.Get(openAICodexTurnStateHeader))
		return codexTicketCompletedResponse(old.Model, fresh), nil
	}}
	svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
	require.Equal(t, []string{"", fresh}, states, "expired credentials must be replaced and the new ticket verified")
	next := svc.lookupOpenAICodexTicket(account, old.Model)
	require.NotNil(t, next)
	require.Equal(t, fresh, next.State)
	require.True(t, next.Verified)
}

func TestCodexTicketRevalidationRejectedOldTicketIsReplacedOnNextEligibleCycle(t *testing.T) {
	svc, account, old := codexRevalidationFixture(t, "")
	fresh := codexTicketStateForExpiryTest(time.Now(), 10)
	var states []string
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		state := req.Header.Get(openAICodexTurnStateHeader)
		states = append(states, state)
		if state == old.State {
			return codexTicketCompletedResponse("gpt-5.6-luna", ""), nil
		}
		return codexTicketCompletedResponse(old.Model, fresh), nil
	}}
	svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
	require.Equal(t, []string{old.State}, states)
	require.True(t, svc.codexTicketRevoked(openAICodexTicketKey(account.ID, old.Model), old))
	require.True(t, svc.openAICodexTicketBackoffActive(account, "tok", old.Model))
	svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
	require.Len(t, states, 1, "automatic replacement must honor the configured retry interval")
	key := codexTicketBackoffKey(account, "tok", old.Model)
	svc.openaiCodexTicketBackoff.Store(key, &codexTicketBackoffState{RetryAt: time.Now().Add(-time.Second), UpdatedAt: time.Now()})
	svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
	require.Equal(t, []string{old.State, "", fresh}, states)
	next := svc.lookupOpenAICodexTicket(account, old.Model)
	require.NotNil(t, next)
	require.Equal(t, fresh, next.State)
	require.True(t, next.Verified)
}
