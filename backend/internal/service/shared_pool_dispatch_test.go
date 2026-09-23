//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedDispatchSettlementRange(t *testing.T) {
	for _, value := range []float64{-1, 101, math.Inf(1), math.NaN()} {
		require.False(t, ValidSharedPoolSettlementMultiplier(value))
	}
}

func TestSharedDispatchCreateRequiresExplicitConsentAndSupportsPendingAssignment(t *testing.T) {
	for _, consent := range []bool{false, true} {
		r := &sharedTierSettingsRepo{cfg: &SharedPoolSettings{MaxConcurrency: 10, SettlementMultiplier: 1.5}}
		s := &SharedPoolService{repo: r, accounts: sharedTierCreatedAccounts{repo: r}, earnings: sharedPoolTotalsStub{}}
		input := SharedPoolAccountInput{Name: "supply", Platform: PlatformGemini, Type: AccountTypeAPIKey, Concurrency: 1, Enabled: true, DispatchConsent: consent, Credentials: map[string]any{"api_key": "test"}}
		_, err := s.Create(context.Background(), 7, input)
		if !consent {
			require.Error(t, err)
			require.Nil(t, r.created)
			input.Enabled = false
			_, err = s.Create(context.Background(), 7, input)
			require.NoError(t, err)
			require.Equal(t, false, r.created.Extra[SharedPoolDispatchConsentKey])
			require.NotContains(t, r.created.Extra, SharedPoolSettlementMultiplierKey)
			continue
		}
		require.NoError(t, err)
		require.Empty(t, r.created.GroupIDs)
		require.NotContains(t, r.created.Extra, SharedPoolSettlementMultiplierKey, "account must not freeze the owner's settlement rate")
	}
}

func TestSharedDispatchUpgradeKeepsAssignmentsAndCannotModifyRates(t *testing.T) {
	r := &sharedTierSettingsRepo{cfg: &SharedPoolSettings{SettlementMultiplier: 2}}
	r.record = SharedPoolAccountRecord{OwnerUserID: 7, AccountID: 1, Assigned: true}
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, GroupIDs: []int64{4}}
	s := &SharedPoolService{repo: r, accounts: sharedPoolAccountRepoStub{account: a}}
	require.NoError(t, s.SetEnabled(context.Background(), 7, 1, true, new(true)))
	require.Equal(t, true, *r.state.DispatchConsent)
	require.Nil(t, r.state.GroupIDs)
	require.ErrorIs(t, s.SetEnabled(context.Background(), 8, 1, true, new(true)), ErrSharedPoolAccountNotFound)
	a.Extra = map[string]any{SharedPoolDispatchConsentKey: true, SharedPoolSettlementMultiplierKey: 3.0}
	require.NoError(t, s.SetEnabled(context.Background(), 7, 1, true, new(true)))
	require.Equal(t, 3.0, a.Extra[SharedPoolSettlementMultiplierKey])
	require.NoError(t, s.SetEnabled(context.Background(), 7, 1, false, nil))
	require.Nil(t, r.state.DispatchConsent)
	require.Nil(t, r.state.GroupIDs)
	for _, consent := range []*bool{new(false), new(true)} {
		require.Error(t, s.SetEnabled(context.Background(), 7, 1, false, consent))
	}
}

func TestSharedDispatchAssignmentDoesNotExpandLegacyScope(t *testing.T) {
	g := sharedTierGroup(4, PlatformOpenAI)
	g.IsSharedPool = false
	s := &SharedPoolService{groups: sharedTierGroups{items: map[int64]*Group{4: g}}}
	cfg := &SharedPoolSettings{DefaultGroupIDs: SharedPoolDefaultGroupIDs{PlatformOpenAI: {4}}}
	require.NoError(t, s.validateSharedGroupSettings(context.Background(), cfg))
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	_, err := s.initialSharedGroups(context.Background(), cfg, a)
	require.Error(t, err)
	a.Extra = map[string]any{SharedPoolDispatchConsentKey: true}
	ids, err := s.initialSharedGroups(context.Background(), cfg, a)
	require.NoError(t, err)
	require.Equal(t, []int64{4}, ids)
}
