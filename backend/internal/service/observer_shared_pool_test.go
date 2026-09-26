//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestObserverSharedPoolGroupScope(t *testing.T) {
	account := observerSharedPoolAccount()
	for _, tc := range []struct {
		name   string
		grants []int64
		allow  bool
	}{
		{"nil_grants", nil, false},
		{"empty_grants", []int64{}, false},
		{"unassigned_group", []int64{9}, false},
		{"assigned_group", []int64{7}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := WithObserverScope(context.Background(), tc.grants)
			_, scoped := ObserverGroupIDs(ctx)
			require.True(t, scoped, "an empty observer scope must not become administrator access")
			require.Equal(t, tc.allow, ObserverCanManageAccount(ctx, account))
			require.Equal(t, tc.allow, ObserverCanManageGroup(ctx, 7))
			require.False(t, ObserverCanManageAccount(ctx, nil))
			require.ErrorIs(t, ValidateObserverGroupBindings(ctx, nil), ErrObserverScope)
			require.ErrorIs(t, ValidateObserverGroupBindings(ctx, []int64{99}), ErrObserverScope)
			if tc.allow {
				require.NoError(t, ValidateObserverGroupBindings(ctx, []int64{7}))
				require.Equal(t, []int64{7}, ObserverVisibleGroups(ctx, account.GroupIDs))
			} else {
				require.ErrorIs(t, ValidateObserverGroupBindings(ctx, []int64{7}), ErrObserverScope)
				require.Empty(t, ObserverVisibleGroups(ctx, account.GroupIDs))
			}
			require.Equal(t, []int64{7, 8}, account.GroupIDs, "response filtering must preserve other group bindings")
		})
	}
}

func TestObserverSharedPoolOrdinaryEditPreservesSettlementAndPause(t *testing.T) {
	ctx := WithObserverScope(context.Background(), []int64{7})
	current := observerSharedPoolAccount()
	require.True(t, ObserverCanManageAccount(ctx, current), "explicit group grants also cover shared accounts")
	for _, tc := range []struct {
		name     string
		incoming map[string]any
	}{
		{"omitted_managed_fields", map[string]any{"privacy_mode": PrivacyModeTrainingOff}},
		{"attempted_managed_override", map[string]any{
			SharedPoolOwnerKey: int64(99), SharedPoolEnabledKey: true, SharedPoolAdminDisabledKey: true,
			SharedPoolDispatchConsentKey: false, SharedPoolSettlementMultiplierKey: 99.0,
			SharedPoolSubscriptionTierKey: "free", "privacy_mode": PrivacyModeTrainingOff,
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			updated := *current
			updated.Extra = PreserveAccountProtection(ctx, current, tc.incoming)
			for key, value := range current.Extra {
				require.Equal(t, value, updated.Extra[key], key)
			}
			require.Equal(t, PrivacyModeTrainingOff, updated.Extra["privacy_mode"])
			require.NotContains(t, current.Extra, "privacy_mode", "ordinary edits must not mutate the cached account")
			require.True(t, updated.Schedulable)
			require.True(t, SharedPoolDispatchConsented(&updated))
			require.False(t, SharedPoolSharingAllowed(&updated))
			require.False(t, updated.IsSchedulable(), "management grants and schedulable cannot undo the supplier pause")
			require.False(t, updated.IsCredentialUsableForShadow())
		})
	}
}

func observerSharedPoolAccount() *Account {
	return &Account{
		ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, GroupIDs: []int64{7, 8},
		Extra: map[string]any{
			SharedPoolOwnerKey: int64(42), SharedPoolEnabledKey: false, SharedPoolAdminDisabledKey: false,
			SharedPoolDispatchConsentKey: true, SharedPoolSettlementMultiplierKey: 1.5, SharedPoolSubscriptionTierKey: "pro",
		},
	}
}
