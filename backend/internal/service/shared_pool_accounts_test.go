//go:build unit

package service

import (
	"context"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"
)

type sharedPoolRepoStub struct {
	SharedPoolRepository
	record       SharedPoolAccountRecord
	stateGroups  *[]int64
	settingsRead int
	stateCalls   int
	state        SharedPoolAccountState
	updateCalls  int
	lastUpdate   SharedPoolAccountUpdate
}

func (r *sharedPoolRepoStub) GetSharedAccount(_ context.Context, ownerID, id int64) (*SharedPoolAccountRecord, error) {
	if ownerID != r.record.OwnerUserID || id != r.record.AccountID {
		return nil, ErrSharedPoolAccountNotFound
	}
	return &r.record, nil
}
func (r *sharedPoolRepoStub) SharedSettings(context.Context) (*SharedPoolSettings, error) {
	r.settingsRead++
	return &SharedPoolSettings{MaxConcurrency: 10, DefaultGroupIDs: SharedPoolDefaultGroupIDs{PlatformOpenAI: {10}}}, nil
}
func (r *sharedPoolRepoStub) SharedUserRates(context.Context) ([]SharedPoolUserRate, error) {
	return nil, nil
}

type sharedPoolTokenInvalidatorStub struct{ projects []string }

func (r *sharedPoolTokenInvalidatorStub) InvalidateToken(_ context.Context, a *Account) error {
	r.projects = append(r.projects, a.GetCredential("project_id"))
	return nil
}

type sharedPoolSequenceAccounts struct {
	AccountRepository
	calls int
}

func (r *sharedPoolSequenceAccounts) GetByID(context.Context, int64) (*Account, error) {
	r.calls++
	project := "old-project"
	if r.calls > 1 {
		project = "new-project"
	}
	return &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"project_id": project}}, nil
}

type sharedPoolTotalsStub struct{ SharedPoolEarningsRepository }

func (sharedPoolTotalsStub) AccountTotals(context.Context, int64, []int64) (map[int64]SharedPoolAccountEarnings, error) {
	return map[int64]SharedPoolAccountEarnings{}, nil
}

func TestSharedPoolReauthorizationInvalidatesOldAndNewProjectTokens(t *testing.T) {
	r := &sharedPoolRepoStub{record: SharedPoolAccountRecord{OwnerUserID: 7, AccountID: 1}}
	invalidator := &sharedPoolTokenInvalidatorStub{}
	s := &SharedPoolService{repo: r, accounts: &sharedPoolSequenceAccounts{}, invalidator: invalidator, earnings: sharedPoolTotalsStub{}}
	_, err := s.Update(context.Background(), 7, 1, SharedPoolAccountInput{Name: "updated", Platform: PlatformGemini, Type: AccountTypeOAuth, Concurrency: 3, Credentials: map[string]any{"access_token": "new", "project_id": "new-project"}})
	require.NoError(t, err)
	require.Equal(t, []string{"old-project", "new-project"}, invalidator.projects)
}
func (r *sharedPoolRepoStub) SetSharedAccountState(_ context.Context, _ int64, state SharedPoolAccountState) error {
	r.stateCalls++
	r.stateGroups = state.GroupIDs
	r.state = state
	return nil
}

type sharedPoolAccountRepoStub struct {
	AccountRepository
	account *Account
}

func (r *sharedPoolRepoStub) UpdateSharedAccount(_ context.Context, _ int64, _ int64, in SharedPoolAccountUpdate) error {
	r.updateCalls++
	r.lastUpdate = in
	return nil
}

func TestSharedPoolInvalidProxyDoesNotReplaceCredentials(t *testing.T) {
	r := &sharedPoolRepoStub{record: SharedPoolAccountRecord{OwnerUserID: 7, AccountID: 1}}
	s := &SharedPoolService{repo: r, accounts: sharedPoolAccountRepoStub{account: &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}}}
	proxy := "http://127.0.0.1:8080"
	_, err := s.Update(context.Background(), 7, 1, SharedPoolAccountInput{Name: "new", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 3, ProxyURL: &proxy, Credentials: map[string]any{"access_token": "new"}})
	require.Error(t, err)
	require.Zero(t, r.updateCalls)
}

func (r sharedPoolAccountRepoStub) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func TestSharedPoolOwnerAndDefaultAssignment(t *testing.T) {
	for _, tc := range []struct {
		name                                            string
		owner                                           int64
		groups                                          []int64
		assigned, adminDisabled, wantError, wantDefault bool
	}{
		{name: "first enable", owner: 7, wantDefault: true},
		{name: "restore A and B", owner: 7, groups: []int64{10, 11}, assigned: true},
		{name: "admin removed all assignments", owner: 7, assigned: true},
		{name: "different owner", owner: 8, wantError: true},
		{name: "admin stopped account", owner: 7, adminDisabled: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &sharedPoolRepoStub{record: SharedPoolAccountRecord{OwnerUserID: 7, AccountID: 1, Assigned: tc.assigned, AdminDisabled: tc.adminDisabled}}
			s := &SharedPoolService{repo: repo, accounts: sharedPoolAccountRepoStub{account: &Account{ID: 1, Platform: PlatformOpenAI, GroupIDs: tc.groups}},
				groups: sharedTierGroups{items: map[int64]*Group{10: sharedTierGroup(10, PlatformOpenAI)}}}
			err := s.SetEnabled(context.Background(), tc.owner, 1, true, nil)
			if tc.wantError {
				require.Error(t, err)
				require.Zero(t, repo.stateCalls)
				return
			}
			require.NoError(t, err)
			if tc.wantDefault {
				require.Equal(t, []int64{10}, *repo.stateGroups)
			} else {
				require.Nil(t, repo.stateGroups)
				require.Zero(t, repo.settingsRead)
			}
		})
	}
}

func TestSharedPoolCredentialBoundary(t *testing.T) {
	clean, err := sanitizeSharedCredentials(PlatformOpenAI, AccountTypeOAuth, map[string]any{"access_token": " token ", "base_url": "http://127.0.0.1", "headers": map[string]any{"Host": "local"}, "rate_multiplier": 0})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"access_token": "token"}, clean)
	_, err = sanitizeSharedCredentials(PlatformOpenAI, AccountTypeOAuth, map[string]any{"access_token": 12})
	require.Error(t, err)
	_, err = sanitizeSharedCredentials(PlatformAntigravity, AccountTypeAPIKey, map[string]any{"api_key": "fake"})
	require.Error(t, err)
	a, _ := sharedCredentialFingerprint(PlatformOpenAI, AccountTypeOAuth, map[string]any{"refresh_token": "stable", "access_token": "old"})
	b, _ := sharedCredentialFingerprint(PlatformOpenAI, AccountTypeOAuth, map[string]any{"refresh_token": "stable", "access_token": "new"})
	require.Equal(t, a, b)
}

func TestSharedPoolProxyRejectsPrivateAddresses(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "10.0.0.1", "192.168.1.2", "172.16.0.1", "169.254.169.254", "100.64.1.2", "fc00::1", "198.18.0.1"} {
		require.False(t, sharedProxyPublicIP(netip.MustParseAddr(ip)), ip)
	}
	require.True(t, sharedProxyPublicIP(netip.MustParseAddr("8.8.8.8")))
	for _, raw := range []string{"http://127.0.0.1:8080", "http://10.0.0.1:80", "ftp://8.8.8.8:21", "http://8.8.8.8", "http://8.8.8.8:80/path", "http://8.8.8.8:80?x=y"} {
		_, err := parseSharedProxy(context.Background(), raw)
		require.Error(t, err, raw)
	}
}

func TestSharedPoolViewDoesNotExposeCredentials(t *testing.T) {
	a := &Account{ID: 1, Name: "mine", Credentials: map[string]any{"access_token": "secret"}, Extra: map[string]any{}, ErrorMessage: "proxy password secret"}
	view := sharedAccountView(a, SharedPoolAccountRecord{OwnerUserID: 9, OwnerEmail: "private@example.com"}, 2000, 100, false)
	require.Zero(t, view.OwnerUserID)
	require.Empty(t, view.OwnerEmail)
	require.NotContains(t, view.ErrorMessage, "secret")
}
