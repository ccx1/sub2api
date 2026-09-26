//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestSharedProTicketSwitchHonorsStoredValue(t *testing.T) {
	for _, tier := range []string{"pro", "chatgpt_pro", "prolite", "pro5", "pro20"} {
		t.Run(tier, func(t *testing.T) {
			a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
				Credentials: map[string]any{"plan_type": tier}, Extra: map[string]any{SharedPoolOwnerKey: int64(7), OpenAICodexTicketEnabledExtraKey: false}}
			require.False(t, OpenAICodexTicketAccountEnabled(a))
			require.Nil(t, NewSharedPoolTicketAccountSnapshot(a, time.Now()))
			s, repo := newSharedTicketService(a)
			for _, enabled := range []bool{true, false} {
				err := s.SetCodexTicketEnabled(context.Background(), 7, 1, enabled)
				require.Equal(t, "SHARED_CODEX_TICKET_ADMIN_ONLY", infraerrors.Reason(err))
				require.Zero(t, repo.writes)
				require.False(t, OpenAICodexTicketAccountEnabled(a))
			}
			for _, enabled := range []bool{true, false} {
				require.NoError(t, s.admin.UpdateAccountExtra(context.Background(), a.ID, map[string]any{OpenAICodexTicketEnabledExtraKey: enabled}))
				require.Equal(t, enabled, OpenAICodexTicketAccountEnabled(a))
				view := sharedAccountView(a, SharedPoolAccountRecord{}, 0, 0, false)
				require.False(t, view.CodexTicketRequired)
				require.Equal(t, new(enabled), view.CodexTicketEnabled)
				encoded, err := json.Marshal(view)
				require.NoError(t, err)
				require.Contains(t, string(encoded), `"codex_ticket_required":false`)
			}
			require.Equal(t, 2, repo.writes)
		})
	}
}

func TestSharedProAdministratorTierDoesNotOverrideTicketSwitch(t *testing.T) {
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"plan_type": "free"},
		Extra: map[string]any{SharedPoolOwnerKey: int64(7), SharedPoolSubscriptionTierKey: "prolite", OpenAICodexTicketEnabledExtraKey: false}}
	require.False(t, OpenAICodexTicketAccountEnabled(a))
	a.Extra[SharedPoolSubscriptionTierKey] = "free"
	a.Credentials["plan_type"] = "pro"
	require.False(t, OpenAICodexTicketAccountEnabled(a))
	a.Extra[SharedPoolSubscriptionTierKey] = "pro"
	require.False(t, OpenAICodexTicketAccountEnabled(a))
	delete(a.Extra, OpenAICodexTicketEnabledExtraKey)
	require.True(t, OpenAICodexTicketAccountEnabled(a), "旧账号缺失开关时仍保留原运行默认")
}

func TestSharedProCreatePreservesExplicitTicketChoice(t *testing.T) {
	for _, tier := range []string{"pro", "prolite"} {
		for _, enabled := range []bool{false, true} {
			accounts := &sharedTicketAccounts{}
			repo := &sharedTicketCreateRepo{accounts: accounts}
			s := &SharedPoolService{repo: repo, accounts: accounts, earnings: sharedPoolTotalsStub{}}
			view, err := s.Create(context.Background(), 7, SharedPoolAccountInput{Name: "mine", Platform: PlatformOpenAI,
				Type: AccountTypeOAuth, Concurrency: 3, CodexTicketEnabled: new(enabled), Credentials: map[string]any{"access_token": "test-token", "plan_type": tier}})
			require.NoError(t, err)
			require.Equal(t, enabled, accounts.account.Extra[OpenAICodexTicketEnabledExtraKey], tier)
			require.False(t, view.CodexTicketRequired)
			require.Equal(t, new(enabled), view.CodexTicketEnabled)
		}
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

func TestSharedProReauthorizationDoesNotChangeTicketChoice(t *testing.T) {
	repo := &sharedRequiredTicketUpdateRepo{sharedPoolRepoStub: sharedPoolRepoStub{record: SharedPoolAccountRecord{OwnerUserID: 7, AccountID: 1}}}
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"plan_type": "free"},
		Extra: map[string]any{SharedPoolOwnerKey: int64(7), OpenAICodexTicketEnabledExtraKey: false, ProxyModeExtraKey: "random"}}
	s := &SharedPoolService{repo: repo, accounts: sharedPoolAccountRepoStub{account: a}, earnings: sharedPoolTotalsStub{}}
	view, err := s.Update(context.Background(), 7, 1, SharedPoolAccountInput{Name: "mine", Platform: PlatformOpenAI,
		Type: AccountTypeOAuth, Concurrency: 2, Credentials: map[string]any{"access_token": "test-token", "plan_type": "prolite"}})
	require.NoError(t, err)
	require.Equal(t, SharedPoolAccountUpdate{Name: "mine", Concurrency: 2,
		Credentials: repo.input.Credentials, Fingerprint: repo.input.Fingerprint}, repo.input, "重授权只更新资料，不附加打票开关变更")
	require.Equal(t, "prolite", repo.input.Credentials["plan_type"])
	require.Equal(t, false, a.Extra[OpenAICodexTicketEnabledExtraKey])
	require.Equal(t, new(false), view.CodexTicketEnabled)
	require.False(t, view.CodexTicketRequired)
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
