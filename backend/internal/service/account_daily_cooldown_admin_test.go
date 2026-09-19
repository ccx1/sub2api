//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDailyCooldownAdminRejectsInvalidWrites(t *testing.T) {
	invalid := dailyCooldownExtra("23:00", "23:00", "Asia/Shanghai")
	account := &Account{ID: 19, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{19: account}}
	svc := &adminServiceImpl{accountRepo: repo}
	_, err := buildAccountForCreate(&CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, invalid)
	require.Error(t, err)
	_, err = svc.UpdateAccount(context.Background(), 19, &UpdateAccountInput{Extra: invalid})
	require.Error(t, err)
	require.Error(t, svc.UpdateAccountExtra(context.Background(), 19, invalid))
	_, err = svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{19}, Extra: invalid})
	require.Error(t, err)
	require.Nil(t, account.Extra)
}

func TestDailyCooldownAdminPreservesExistingExtra(t *testing.T) {
	extra := dailyCooldownExtra("23:00", "08:00", "Asia/Shanghai")
	extra["custom"] = "kept"
	account, err := buildAccountForCreate(&CreateAccountInput{Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, extra)
	require.NoError(t, err)
	require.Equal(t, "kept", account.Extra["custom"])
	require.Equal(t, extra[DailyCooldownExtraKey], account.Extra[DailyCooldownExtraKey])
	account.ID = 19
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{19: account}}
	svc := &adminServiceImpl{accountRepo: repo}
	require.NoError(t, svc.UpdateAccountExtra(context.Background(), 19, map[string]any{
		DailyCooldownExtraKey: map[string]any{"enabled": false},
	}))
	require.Equal(t, "kept", repo.accounts[19].Extra["custom"])
	require.Equal(t, map[string]any{"enabled": false}, repo.accounts[19].Extra[DailyCooldownExtraKey])
}
