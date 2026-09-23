package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProtectionEnablePreservesCodexTicketWithAccount(t *testing.T) {
	ctx := context.Background()
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken} {
		t.Run(accountType, func(t *testing.T) {
			repo := &protectionSyncAdminRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
				1: {ID: 1, Platform: PlatformOpenAI, Type: accountType, Concurrency: 4, Extra: map[string]any{
					OpenAICodexTicketEnabledExtraKey: false, "custom": "preserved",
				}},
			}}}
			svc := NewAntiDegradeService(&adminServiceImpl{accountRepo: repo})
			enabled, err := svc.SetProtection(ctx, 1, true, false)
			require.NoError(t, err)
			require.True(t, enabled.AntiDegradationEnabled())
			require.False(t, OpenAICodexTicketAccountEnabled(enabled))
			persisted, err := repo.GetByID(ctx, 1)
			require.NoError(t, err)
			require.Equal(t, false, persisted.Extra[OpenAICodexTicketEnabledExtraKey])
			require.Equal(t, "preserved", persisted.Extra["custom"])
			require.Equal(t, 1, repo.managedWrites)
		})
	}
}

func TestProtectionEnableLeavesExistingCodexTicketSwitchUnchanged(t *testing.T) {
	ctx := context.Background()
	store := newProtectionSettingsAccountStore()
	svc := NewAntiDegradeService(store)
	_, err := svc.ApplyAntiDegradeMode(ctx, 1, AntiDegradeModeLegacy)
	require.NoError(t, err)
	store.account.Extra[OpenAICodexTicketEnabledExtraKey] = false
	updated, err := svc.SetProtection(ctx, 1, true, false)
	require.NoError(t, err)
	require.Equal(t, false, updated.Extra[OpenAICodexTicketEnabledExtraKey])
	require.Equal(t, "legacy", updated.ProtectionMode())
	require.Equal(t, 1, store.writes)
	_, err = svc.SetProtection(ctx, 1, true, false)
	require.NoError(t, err)
	require.Equal(t, 1, store.writes, "重复开启保护不得改变打票选择或额外写入")
}

func TestProtectionDisablePreservesCodexTicketChoice(t *testing.T) {
	ctx := context.Background()
	for _, ticketEnabled := range []bool{true, false} {
		t.Run(map[bool]string{true: "ticket_on", false: "ticket_off"}[ticketEnabled], func(t *testing.T) {
			store := newProtectionSettingsAccountStore()
			svc := NewAntiDegradeService(store)
			_, err := svc.SetProtection(ctx, 1, true, false)
			require.NoError(t, err)
			store.account.Extra[OpenAICodexTicketEnabledExtraKey] = ticketEnabled
			updated, err := svc.SetProtection(ctx, 1, false, true)
			require.NoError(t, err)
			require.False(t, updated.AntiDegradationEnabled())
			require.Equal(t, ticketEnabled, updated.Extra[OpenAICodexTicketEnabledExtraKey])
		})
	}
}

func TestProtectionEnableDoesNotChangeUnsupportedTicketAccounts(t *testing.T) {
	for _, kind := range []string{"anthropic", "api_key", "shadow"} {
		t.Run(kind, func(t *testing.T) {
			store := newProtectionSettingsAccountStore()
			store.account.Extra[OpenAICodexTicketEnabledExtraKey] = false
			switch kind {
			case "anthropic":
				store.account.Platform = PlatformAnthropic
			case "api_key":
				store.account.Type = AccountTypeAPIKey
			case "shadow":
				store.account.ParentAccountID = new(int64(9))
			}
			updated, err := NewAntiDegradeService(store).SetProtection(context.Background(), 1, true, false)
			require.NoError(t, err)
			require.True(t, updated.AntiDegradationEnabled())
			require.Equal(t, false, updated.Extra[OpenAICodexTicketEnabledExtraKey])
		})
	}
}

func TestProtectionEnableTicketWriteFailureLeavesAccountUnchanged(t *testing.T) {
	store := newProtectionSettingsAccountStore()
	store.account.Extra[OpenAICodexTicketEnabledExtraKey] = false
	store.writeErr = errors.New("account update failed")
	_, err := NewAntiDegradeService(store).SetProtection(context.Background(), 1, true, false)
	require.ErrorIs(t, err, store.writeErr)
	require.False(t, store.account.AntiDegradationEnabled())
	require.Equal(t, false, store.account.Extra[OpenAICodexTicketEnabledExtraKey])
	require.Zero(t, store.writes)
}

func TestProtectionSyncPreservesExplicitTicketDisable(t *testing.T) {
	ctx := context.Background()
	repo := &protectionSyncAdminRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{
		1: {ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 4, Extra: map[string]any{}},
	}}}
	admin := &adminServiceImpl{accountRepo: repo}
	svc := NewAntiDegradeServiceWithSettings(admin, &protectionSettingsRepoStub{}, repo)
	_, err := svc.SetProtection(ctx, 1, true, false)
	require.NoError(t, err)
	updated, err := admin.UpdateAccount(ctx, 1, &UpdateAccountInput{Extra: map[string]any{OpenAICodexTicketEnabledExtraKey: false}})
	require.NoError(t, err)
	require.True(t, updated.AntiDegradationEnabled())
	require.False(t, OpenAICodexTicketAccountEnabled(updated))
	result, err := svc.UpdateProtectionSettings(ctx, AntiDegradeModeLegacy)
	require.NoError(t, err)
	require.Equal(t, 1, result.Sync.Updated)
	persisted, err := repo.GetByID(ctx, 1)
	require.NoError(t, err)
	require.True(t, persisted.AntiDegradationEnabled())
	require.False(t, OpenAICodexTicketAccountEnabled(persisted))
	require.Equal(t, false, persisted.Extra[OpenAICodexTicketEnabledExtraKey])
}
