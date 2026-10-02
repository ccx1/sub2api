package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketRevalidationSkipsConsumedPendingCookies(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	a, b := usageTestTicket("A", 30*time.Second, now), usageTestTicket("B", 20*time.Second, now)
	for _, ticket := range []*openAICodexTicket{a, b} {
		ticket.Verified, ticket.RevalidateAt = true, now.Add(-time.Second)
	}
	svc := usageTestService(t, config.OpenAICodexTicketConfig{FailClosed: true, ConsumeAfterUse: true}, a, b)
	cfg := svc.openAICodexTicketConfig()
	svc.openaiCodexTicketPending.Store(openAICodexTicketKey(account.ID, usageTestModel), &codexTicketPendingCookies{
		Ticket: codexTicketLeaf(a), Candidate: &openAICodexTicketCookieCandidate{}, ExpiresAt: now.Add(time.Minute),
	})
	target, pending := svc.codexTicketRevalidationTarget(account, usageTestModel, cfg)
	require.NotNil(t, pending)
	require.Equal(t, a.State, target.State)

	_, state, err := usageTestApply(context.Background(), svc, account)
	require.NoError(t, err)
	require.Equal(t, a.State, state)
	target, pending = svc.codexTicketRevalidationTarget(account, usageTestModel, cfg)
	require.Nil(t, pending)
	require.NotNil(t, target)
	require.Equal(t, b.State, target.State)
}

func TestCodexTicketRevalidationCannotRepublishConsumedTicket(t *testing.T) {
	svc, account, old := codexRevalidationFixture(t, "")
	svc.cfg.Gateway.OpenAICodexTicket.ConsumeAfterUse = true
	cfg := svc.openAICodexTicketConfig()
	next := codexTicketRevalidationSnapshot(old, nil, cfg, time.Now())
	require.NotNil(t, next)
	require.True(t, next.usable(time.Now(), account, cfg))

	receipt, state, err := usageTestApply(context.Background(), svc, account)
	require.NoError(t, err)
	require.Equal(t, old.State, state)
	require.NotNil(t, receipt.claimed)
	require.False(t, svc.replaceRevalidatedCodexTicket(context.Background(), account, old, next, nil))
	require.Nil(t, svc.lookupOpenAICodexTicketForUse(account, old.Model, cfg))
}
