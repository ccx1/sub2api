//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestSharedProTicketRequiredIgnoresHistoricalFalseWithoutChangingOrdinaryAccounts(t *testing.T) {
	for _, tier := range []string{"pro", "chatgpt_pro", "prolite", "pro5", "pro20"} {
		a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
			Credentials: map[string]any{"plan_type": tier}, Extra: map[string]any{SharedPoolOwnerKey: int64(7), OpenAICodexTicketEnabledExtraKey: false}}
		require.True(t, SharedPoolCodexTicketRequired(a), tier)
		require.True(t, OpenAICodexTicketAccountEnabled(a), "legacy false must not bypass runtime collection/gate")
		require.NotNil(t, NewSharedPoolTicketAccountSnapshot(a, time.Now()), "pool capacity must retain ticket gating")
		s, repo := newSharedTicketService(a)
		err := s.SetCodexTicketEnabled(context.Background(), 7, 1, false)
		require.Equal(t, "SHARED_CODEX_TICKET_REQUIRED", infraerrors.Reason(err))
		require.Zero(t, repo.writes)
		view := sharedAccountView(a, SharedPoolAccountRecord{}, 0, 0, false)
		require.True(t, view.CodexTicketRequired)
		require.Equal(t, new(true), view.CodexTicketEnabled)
		delete(a.Extra, SharedPoolOwnerKey)
		require.False(t, SharedPoolCodexTicketRequired(a))
		require.False(t, OpenAICodexTicketAccountEnabled(a), "ordinary account keeps its switch")
	}
}

func TestSharedProTicketRequiredUsesAdministratorTierAndOnlyOAuth(t *testing.T) {
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"plan_type": "free"},
		Extra: map[string]any{SharedPoolOwnerKey: int64(7), SharedPoolSubscriptionTierKey: "prolite", OpenAICodexTicketEnabledExtraKey: false}}
	require.True(t, SharedPoolCodexTicketRequired(a))
	a.Extra[SharedPoolSubscriptionTierKey] = "free"
	a.Credentials["plan_type"] = "pro"
	require.False(t, SharedPoolCodexTicketRequired(a))
	a.Extra[SharedPoolSubscriptionTierKey] = "pro"
	a.Type = AccountTypeAPIKey
	require.False(t, SharedPoolCodexTicketRequired(a))
	a.Type = AccountTypeOAuth
	a.ParentAccountID = new(int64(9))
	require.False(t, SharedPoolCodexTicketRequired(a))
	require.False(t, SharedPoolCodexTicketRequired(nil))
}

func TestSharedProCreateCannotPersistDisabledTickets(t *testing.T) {
	for _, tier := range []string{"pro", "prolite"} {
		accounts := &sharedTicketAccounts{}
		repo := &sharedTicketCreateRepo{accounts: accounts}
		s := &SharedPoolService{repo: repo, accounts: accounts, earnings: sharedPoolTotalsStub{}}
		view, err := s.Create(context.Background(), 7, SharedPoolAccountInput{Name: "mine", Platform: PlatformOpenAI,
			Type: AccountTypeOAuth, Concurrency: 3, CodexTicketEnabled: new(false), Credentials: map[string]any{"access_token": "test-token", "plan_type": tier}})
		require.NoError(t, err)
		require.Equal(t, true, accounts.account.Extra[OpenAICodexTicketEnabledExtraKey])
		require.True(t, view.CodexTicketRequired)
		require.Equal(t, new(true), view.CodexTicketEnabled)
	}
}

type sharedRequiredTicketUpdateRepo struct {
	sharedPoolRepoStub
	input SharedPoolAccountUpdate
}

func (r *sharedRequiredTicketUpdateRepo) UpdateSharedAccount(_ context.Context, _, _ int64, input SharedPoolAccountUpdate) error {
	r.input = input
	return nil
}

func TestSharedProReauthorizationNormalizesTicketFlagInProfileTransaction(t *testing.T) {
	repo := &sharedRequiredTicketUpdateRepo{sharedPoolRepoStub: sharedPoolRepoStub{record: SharedPoolAccountRecord{OwnerUserID: 7, AccountID: 1}}}
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"plan_type": "free"},
		Extra: map[string]any{SharedPoolOwnerKey: int64(7), OpenAICodexTicketEnabledExtraKey: false, ProxyModeExtraKey: "random"}}
	s := &SharedPoolService{repo: repo, accounts: sharedPoolAccountRepoStub{account: a}, earnings: sharedPoolTotalsStub{}}
	_, err := s.Update(context.Background(), 7, 1, SharedPoolAccountInput{Name: "mine", Platform: PlatformOpenAI,
		Type: AccountTypeOAuth, Concurrency: 2, Credentials: map[string]any{"access_token": "test-token", "plan_type": "prolite"}})
	require.NoError(t, err)
	require.True(t, repo.input.ForceCodexTicket)
}

func TestSharedAccountViewKeepsAdministratorRoutingPrivate(t *testing.T) {
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, GroupIDs: []int64{4}, Groups: []*Group{{ID: 4, Name: "private"}},
		Extra: map[string]any{SharedPoolSubscriptionTierKey: "pro"}}
	owner := sharedAccountView(a, SharedPoolAccountRecord{}, 0, 0, false)
	require.Empty(t, owner.GroupIDs)
	require.Empty(t, owner.Groups)
	require.Empty(t, owner.SubscriptionTierOverride)
	require.Equal(t, "pro", owner.SubscriptionTier)
	admin := sharedAccountView(a, SharedPoolAccountRecord{}, 0, 0, true)
	require.Equal(t, []int64{4}, admin.GroupIDs)
	require.Equal(t, "private", admin.Groups[0].Name)
	require.Equal(t, "pro", admin.SubscriptionTierOverride)
}
