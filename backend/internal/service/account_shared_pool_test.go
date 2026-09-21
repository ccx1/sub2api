package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedPoolScheduleGuardSurvivesOrdinarySchedulableChanges(t *testing.T) {
	account := &Account{Status: StatusActive, Schedulable: true, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Extra: map[string]any{SharedPoolOwnerKey: int64(9), SharedPoolEnabledKey: false, SharedPoolAdminDisabledKey: false}}
	require.False(t, account.IsSchedulable())
	require.False(t, account.IsCredentialUsableForShadow())
	account.Extra[SharedPoolEnabledKey] = true
	require.True(t, account.IsSchedulable())
	account.Extra[SharedPoolAdminDisabledKey] = true
	require.False(t, account.IsSchedulable())
	account.Extra = nil
	require.True(t, account.IsSchedulable(), "ordinary accounts retain existing scheduling behavior")
}

func TestSharedPoolOwnershipAndSwitchesSurviveStaleOrManagedUpdates(t *testing.T) {
	current := &Account{Extra: map[string]any{SharedPoolOwnerKey: int64(9), SharedPoolEnabledKey: false, SharedPoolAdminDisabledKey: true, SharedPoolDispatchConsentKey: true, SharedPoolSubscriptionTierKey: "pro"}}
	for _, ctx := range []context.Context{context.Background(), context.WithValue(context.Background(), mode1ManagedWriteKey{}, true)} {
		incoming := map[string]any{SharedPoolOwnerKey: int64(99), SharedPoolEnabledKey: true, SharedPoolAdminDisabledKey: false, SharedPoolDispatchConsentKey: false, SharedPoolSubscriptionTierKey: "free", "privacy_mode": PrivacyModeTrainingOff}
		result := PreserveAccountProtection(ctx, current, incoming)
		require.EqualValues(t, 9, result[SharedPoolOwnerKey])
		require.Equal(t, false, result[SharedPoolEnabledKey])
		require.Equal(t, true, result[SharedPoolAdminDisabledKey])
		require.Equal(t, true, result[SharedPoolDispatchConsentKey])
		require.Equal(t, "pro", result[SharedPoolSubscriptionTierKey])
		require.Equal(t, PrivacyModeTrainingOff, result["privacy_mode"])
		result = PreserveAccountProtection(ctx, &Account{}, incoming)
		require.NotContains(t, result, SharedPoolOwnerKey)
		require.NotContains(t, result, SharedPoolEnabledKey)
		require.NotContains(t, result, SharedPoolAdminDisabledKey)
		require.NotContains(t, result, SharedPoolDispatchConsentKey)
		require.NotContains(t, result, SharedPoolSubscriptionTierKey)
	}
}
