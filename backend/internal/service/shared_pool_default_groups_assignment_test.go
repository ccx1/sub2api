//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedDefaultGroupsCreateAndFirstEnableAssignEveryGroup(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		r := &sharedTierSettingsRepo{cfg: &SharedPoolSettings{MaxConcurrency: 10, SettlementMultiplier: 1,
			DefaultGroupIDs: SharedPoolDefaultGroupIDs{PlatformGemini: {10, 11, 10}}}}
		s := &SharedPoolService{repo: r, accounts: sharedTierCreatedAccounts{repo: r}, earnings: sharedPoolTotalsStub{}, groups: sharedTierGroups{items: map[int64]*Group{
			10: sharedTierGroup(10, PlatformGemini), 11: sharedTierGroup(11, PlatformGemini),
		}}}
		_, err := s.Create(context.Background(), 7, SharedPoolAccountInput{Name: "multi-default", Platform: PlatformGemini, Type: AccountTypeAPIKey,
			Concurrency: 1, Enabled: enabled, DispatchConsent: enabled, Credentials: map[string]any{"api_key": "test"}})
		require.NoError(t, err)
		if enabled {
			require.Equal(t, []int64{10, 11}, r.created.GroupIDs)
		} else {
			require.Empty(t, r.created.GroupIDs)
			require.NoError(t, s.SetEnabled(context.Background(), 7, 1, true, new(true)))
			require.Equal(t, []int64{10, 11}, *r.stateGroups)
		}
	}
}

func TestSharedDefaultGroupsAdminEnableAndTierPrecedence(t *testing.T) {
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Extra: map[string]any{SharedPoolDispatchConsentKey: true}}
	s, repo := sharedAdminStateService(a)
	repo.cfg.DefaultGroupIDs[PlatformOpenAI] = []int64{10, 11}
	require.NoError(t, s.AdminSetAccountState(context.Background(), 1, SharedPoolAccountState{Enabled: new(true)}))
	require.Equal(t, []int64{10, 11}, repo.state.DefaultGroupIDs)
	require.Nil(t, repo.state.GroupIDs)
	require.NoError(t, s.AdminSetAccountState(context.Background(), 1, SharedPoolAccountState{Enabled: new(true), SubscriptionTier: new("pro")}))
	require.Equal(t, []int64{11}, repo.state.DefaultGroupIDs, "订阅档位规则继续覆盖全部默认分组")
}

func TestSharedDefaultGroupsSaveValidatesEveryGroup(t *testing.T) {
	for _, id := range []int64{11, 99} {
		r := &sharedTierSettingsRepo{}
		s := &SharedPoolService{repo: r, groups: sharedTierGroups{items: map[int64]*Group{
			10: sharedTierGroup(10, PlatformOpenAI), 11: sharedTierGroup(11, PlatformOpenAI),
		}}}
		settings := &SharedPoolSettings{MaxConcurrency: 10, SettlementMultiplier: 1,
			DefaultGroupIDs: SharedPoolDefaultGroupIDs{PlatformOpenAI: {10, id, 10}}}
		err := s.SaveSettings(context.Background(), settings)
		if id == 99 {
			require.Error(t, err)
			require.False(t, r.saved)
		} else {
			require.NoError(t, err)
			require.True(t, r.saved)
			require.Equal(t, []int64{10, 11}, r.cfg.DefaultGroupIDs[PlatformOpenAI])
		}
	}
}
