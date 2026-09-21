//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type sharedAdminStateRepo struct{ sharedTierSettingsRepo }

func (r *sharedAdminStateRepo) GetSharedAccount(_ context.Context, ownerID, id int64) (*SharedPoolAccountRecord, error) {
	if ownerID != 0 || id != r.record.AccountID {
		return nil, ErrSharedPoolAccountNotFound
	}
	return &r.record, nil
}

func sharedAdminStateService(account *Account) (*SharedPoolService, *sharedAdminStateRepo) {
	repo := &sharedAdminStateRepo{}
	repo.record = SharedPoolAccountRecord{OwnerUserID: 7, AccountID: account.ID}
	repo.cfg = &SharedPoolSettings{DefaultGroupIDs: map[string]int64{PlatformOpenAI: 10}, SubscriptionGroupIDs: map[string]map[string]int64{PlatformOpenAI: {"pro": 11}}}
	return &SharedPoolService{repo: repo, accounts: sharedPoolAccountRepoStub{account: account}, groups: sharedTierGroups{items: map[int64]*Group{
		10: sharedTierGroup(10, PlatformOpenAI), 11: sharedTierGroup(11, PlatformOpenAI),
	}}}, repo
}

func TestSharedAdminStateEnableRoutesOnlyUnassignedConsentedAccount(t *testing.T) {
	for _, tc := range []struct {
		name        string
		assigned    bool
		groups      []int64
		modern      bool
		wantDefault bool
	}{
		{"initial default", false, nil, true, true},
		{"assigned empty", true, nil, true, false},
		{"existing groups", false, []int64{8}, true, false},
		{"legacy scope", false, nil, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, GroupIDs: tc.groups, Extra: map[string]any{}}
			if tc.modern {
				a.Extra[SharedPoolDispatchConsentKey] = true
			}
			s, repo := sharedAdminStateService(a)
			repo.record.Assigned, repo.record.AdminDisabled = tc.assigned, true
			require.NoError(t, s.AdminSetAccountState(context.Background(), 1, SharedPoolAccountState{Enabled: new(true), AdminDisabled: new(false), SubscriptionTier: new("pro")}))
			require.Nil(t, repo.state.DispatchConsent)
			require.Nil(t, repo.state.GroupIDs)
			if tc.wantDefault {
				require.Equal(t, new(int64(11)), repo.state.DefaultGroupID)
			} else {
				require.Nil(t, repo.state.DefaultGroupID)
			}
			require.Equal(t, "pro", *repo.state.SubscriptionTier)
			require.NotContains(t, a.Extra, SharedPoolSubscriptionTierKey, "service cannot mutate a cached account snapshot")
		})
	}
}

func TestSharedAdminStateCannotForgeConsentOrChangeInvalidTier(t *testing.T) {
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{SharedPoolDispatchConsentKey: false}}
	s, repo := sharedAdminStateService(a)
	for _, state := range []SharedPoolAccountState{
		{Enabled: new(true)}, {DispatchConsent: new(true)}, {OwnerID: 7}, {DefaultGroupID: new(int64(11))}, {SubscriptionTier: new("invalid-tier")}, {Priority: new(-1)}, {Priority: new(101)},
	} {
		require.Error(t, s.AdminSetAccountState(context.Background(), 1, state))
	}
	require.Zero(t, repo.stateCalls)
	require.NoError(t, s.AdminSetAccountState(context.Background(), 1, SharedPoolAccountState{SubscriptionTier: new("")}))
	require.Equal(t, "", *repo.state.SubscriptionTier)
}
