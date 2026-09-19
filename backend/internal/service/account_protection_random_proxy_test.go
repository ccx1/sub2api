package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProtectionAllowsRandomProxyForIdentityStrategies(t *testing.T) {
	for _, profile := range ListAntiDegradeStrategyProfiles() {
		if !profile.ApplySupported {
			continue
		}
		t.Run(string(profile.ID), func(t *testing.T) {
			ctx := context.Background()
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
				1: {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
					Concurrency: 4, Extra: map[string]any{ProxyModeExtraKey: ProxyModeRandom}},
			}}
			protection := NewAntiDegradeService(&adminServiceImpl{accountRepo: repo})
			account, err := protection.ApplyAntiDegradeMode(ctx, 1, profile.ID)
			require.NoError(t, err)
			require.True(t, account.IsRandomProxy())
			require.True(t, account.AntiDegradationEnabled())
			require.Equal(t, string(profile.ID), account.ProtectionMode())
			require.NoError(t, ValidateAccountProtectionConfiguration(account))
			_, err = resolveMode1TLSProfile(account)
			require.NoError(t, err)
		})
	}
}

func TestProtectionProxyEditAndRevertPreserveIndependentRouting(t *testing.T) {
	for _, mode := range []AntiDegradeMode{AntiDegradeModeLegacy, AntiDegradeMode1} {
		t.Run(string(mode), func(t *testing.T) {
			ctx := context.Background()
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
				1: {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Concurrency: 4, Extra: map[string]any{}},
			}}
			admin := &adminServiceImpl{accountRepo: repo}
			protection := NewAntiDegradeService(admin)
			protected, err := protection.ApplyAntiDegradeMode(ctx, 1, mode)
			require.NoError(t, err)
			seed := protected.Extra[codexFingerprintSeedExtraKey]
			// 旧策略快照可能含 proxy_mode；还原身份时不能覆盖后来修改的出口。
			antiDegradePrev(protected)[ProxyModeExtraKey] = "fixed"
			updated, err := admin.UpdateAccount(ctx, 1, &UpdateAccountInput{Extra: map[string]any{
				ProxyModeExtraKey: ProxyModeRandom, RandomProxyPoolScopeExtraKey: RandomProxyPoolSelected,
				RandomProxyPoolIDsExtraKey: []int64{7, 9}, "custom": true,
			}})
			require.NoError(t, err)
			require.True(t, updated.IsRandomProxy())
			require.Equal(t, string(mode), updated.ProtectionMode())
			require.Equal(t, seed, updated.Extra[codexFingerprintSeedExtraKey])
			updated, err = protection.SetProtection(ctx, 1, false, true)
			require.NoError(t, err)
			require.False(t, updated.AntiDegradationEnabled())
			require.True(t, updated.IsRandomProxy())
			require.Equal(t, []int64{7, 9}, updated.RandomProxyPoolIDs())
		})
	}
}

func TestProtectionNewRandomAccountRetainsIdentityPolicy(t *testing.T) {
	a := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 4,
		Extra: map[string]any{ProxyModeExtraKey: ProxyModeRandom}}
	PrepareNewAccountProtection(a)
	require.True(t, a.IsRandomProxy())
	require.Equal(t, string(DefaultAntiDegradeMode), a.ProtectionMode())
	require.NoError(t, ValidateAccountProtectionConfiguration(a))
	require.NotContains(t, ProtectionManagedKeys(a), ProxyModeExtraKey)
}
