//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type sharedTicketAccounts struct {
	AccountRepository
	account *Account
	reads   int
	writes  int
	updates map[string]any
	err     error
}

func (r *sharedTicketAccounts) GetByID(context.Context, int64) (*Account, error) {
	r.reads++
	return r.account, nil
}

func (r *sharedTicketAccounts) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	if r.err != nil {
		return r.err
	}
	r.writes++
	r.updates = updates
	if r.account.Extra == nil {
		r.account.Extra = map[string]any{}
	}
	for key, value := range updates {
		r.account.Extra[key] = value
	}
	return nil
}

func newSharedTicketService(account *Account) (*SharedPoolService, *sharedTicketAccounts) {
	repo := &sharedPoolRepoStub{record: SharedPoolAccountRecord{AccountID: account.ID, OwnerUserID: 7}}
	accounts := &sharedTicketAccounts{account: account}
	s := &SharedPoolService{repo: repo, accounts: accounts, admin: &adminServiceImpl{accountRepo: accounts}, earnings: sharedPoolTotalsStub{}}
	return s, accounts
}

func TestSharedCodexTicketOwnedSwitchUsesRuntimeStateAndPreservesOtherExtra(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
		SharedPoolOwnerKey: int64(7), SharedPoolEnabledKey: false,
		"codex_turn_ticket:test": map[string]any{"state": "private-ticket"}, "codex_5h_used_percent": 12,
	}}
	s, repo := newSharedTicketService(account)
	require.True(t, OpenAICodexTicketAccountEnabled(account))
	for _, enabled := range []bool{false, true, false} {
		require.NoError(t, s.SetCodexTicketEnabled(context.Background(), 7, 1, enabled))
		require.Equal(t, map[string]any{OpenAICodexTicketEnabledExtraKey: enabled}, repo.updates)
		require.Equal(t, enabled, OpenAICodexTicketAccountEnabled(account))
		view, err := s.Get(context.Background(), 7, 1)
		require.NoError(t, err)
		require.NotNil(t, view.CodexTicketEnabled)
		require.Equal(t, enabled, *view.CodexTicketEnabled)
		encoded, err := json.Marshal(view)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "private-ticket")
		require.NotContains(t, string(encoded), "codex_turn_ticket")
	}
	require.Equal(t, false, account.Extra[SharedPoolEnabledKey])
	require.Equal(t, 12, account.Extra["codex_5h_used_percent"])
	require.Equal(t, map[string]any{"state": "private-ticket"}, account.Extra["codex_turn_ticket:test"])
}

func TestSharedCodexTicketRejectsOtherOwnerAndUnsupportedAccounts(t *testing.T) {
	parentID := int64(2)
	for _, tc := range []struct {
		name     string
		owner    int64
		account  *Account
		wantCode int
	}{
		{"other owner", 8, &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, 404},
		{"API key", 7, &Account{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, 400},
		{"other platform", 7, &Account{Platform: PlatformGemini, Type: AccountTypeOAuth}, 400},
		{"shadow", 7, &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: &parentID}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.account.ID = 1
			s, repo := newSharedTicketService(tc.account)
			err := s.SetCodexTicketEnabled(context.Background(), tc.owner, 1, false)
			require.Equal(t, tc.wantCode, infraerrors.Code(err))
			require.Zero(t, repo.writes)
			if tc.owner != 7 {
				require.Zero(t, repo.reads)
			} else {
				view := sharedAccountView(tc.account, SharedPoolAccountRecord{}, 0, 0, false)
				require.Nil(t, view.CodexTicketEnabled)
				encoded, err := json.Marshal(view)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), "codex_ticket_enabled")
			}
		})
	}
}

func TestSharedCodexTicketSupportsExistingOAuthLikeAndReturnsWriteFailure(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeSetupToken}
	s, repo := newSharedTicketService(account)
	require.NoError(t, s.SetCodexTicketEnabled(context.Background(), 7, 1, false))
	require.False(t, OpenAICodexTicketAccountEnabled(account))
	repo.err = errors.New("write failed")
	require.ErrorIs(t, s.SetCodexTicketEnabled(context.Background(), 7, 1, true), repo.err)
	require.False(t, OpenAICodexTicketAccountEnabled(account))
}

type sharedTicketCreateRepo struct {
	sharedPoolRepoStub
	accounts *sharedTicketAccounts
}

func (r *sharedTicketCreateRepo) CreateSharedAccount(_ context.Context, a *Account, owner int64, _ string) error {
	a.ID = 1
	r.accounts.account = a
	r.record = SharedPoolAccountRecord{AccountID: a.ID, OwnerUserID: owner}
	return nil
}

func TestSharedCodexTicketCreateOptionalExplicitFalseAndUnsupported(t *testing.T) {
	for _, raw := range []string{`{}`, `{"codex_ticket_enabled":false}`, `{"codex_ticket_enabled":true}`} {
		t.Run(raw, func(t *testing.T) {
			in := SharedPoolAccountInput{Name: "mine", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 3,
				Credentials: map[string]any{"access_token": "test-token", "codex_ticket_enabled": false}}
			require.NoError(t, json.Unmarshal([]byte(raw), &in))
			accounts := &sharedTicketAccounts{}
			repo := &sharedTicketCreateRepo{accounts: accounts}
			s := &SharedPoolService{repo: repo, accounts: accounts, earnings: sharedPoolTotalsStub{}}
			view, err := s.Create(context.Background(), 7, in)
			require.NoError(t, err)
			want := in.CodexTicketEnabled == nil || *in.CodexTicketEnabled
			require.Equal(t, want, OpenAICodexTicketAccountEnabled(accounts.account))
			require.Equal(t, want, *view.CodexTicketEnabled)
			_, exists := accounts.account.Extra[OpenAICodexTicketEnabledExtraKey]
			require.Equal(t, in.CodexTicketEnabled != nil, exists)
			require.NotContains(t, accounts.account.Credentials, OpenAICodexTicketEnabledExtraKey)
		})
	}
	disabled := false
	for _, account := range []SharedPoolAccountInput{
		{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{Platform: PlatformAnthropic, Type: AccountTypeOAuth},
	} {
		account.Name, account.Concurrency, account.CodexTicketEnabled = "mine", 3, &disabled
		s := &SharedPoolService{repo: &sharedPoolRepoStub{}}
		_, err := s.Create(context.Background(), 7, account)
		require.Equal(t, "CODEX_TICKET_UNSUPPORTED_ACCOUNT", infraerrors.Reason(err))
	}
}

func TestSharedCodexTicketOrdinaryUpdatesAndReauthorizationUseSeparateSwitch(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
		OpenAICodexTicketEnabledExtraKey: false, ProxyModeExtraKey: "random",
	}}
	s, repo := newSharedTicketService(account)
	in := SharedPoolAccountInput{Name: "mine", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 2}
	for _, credentials := range []map[string]any{nil, {"access_token": "new-token"}} {
		in.Credentials = credentials
		view, err := s.Update(context.Background(), 7, 1, in)
		require.NoError(t, err)
		require.False(t, *view.CodexTicketEnabled)
		require.False(t, OpenAICodexTicketAccountEnabled(account))
	}
	require.Zero(t, repo.writes, "普通更新和重授权不能覆盖独立打票状态")
	for _, enabled := range []bool{false, true} {
		in.CodexTicketEnabled = &enabled
		_, err := s.Update(context.Background(), 7, 1, in)
		require.Equal(t, "SHARED_SWITCH_SEPARATE", infraerrors.Reason(err))
	}
	require.Equal(t, 2, s.repo.(*sharedPoolRepoStub).updateCalls)
}
