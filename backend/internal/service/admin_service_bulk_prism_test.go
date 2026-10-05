//go:build unit

package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

func TestBulkPrismSettingsAndTargetEligibility(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{
			{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
			{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth},
		}}
		result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(t.Context(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2}, Extra: map[string]any{
			PrismBrowserEnabledKey: enabled, PrismBrowserModelsKey: []string{"gpt-6-luna"},
		}})
		require.NoError(t, err)
		require.Equal(t, 2, result.Success)
		require.Equal(t, enabled, repo.lastBulkUpdate.Extra[PrismBrowserEnabledKey])
		wantModels := []string{}
		if enabled {
			wantModels = []string{"gpt-6-luna"}
		}
		require.Equal(t, wantModels, repo.lastBulkUpdate.Extra[PrismBrowserModelsKey])
		require.Len(t, repo.lastBulkUpdate.Extra, 2)
	}
	for _, target := range []*Account{
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: new(int64(1))},
	} {
		repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}, target}}
		_, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(t.Context(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2}, Extra: map[string]any{PrismBrowserEnabledKey: true}})
		require.Error(t, err)
		require.Zero(t, repo.bulkUpdateCalls)
	}
}

func TestBulkPrismFilteredTargetsRejectMissingAndIncompatibleAccounts(t *testing.T) {
	for _, compatible := range []bool{true, false} {
		account := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
		if !compatible {
			account.Type = AccountTypeAPIKey
		}
		repo := &accountRepoStubForBulkUpdate{listData: []Account{{ID: 7}}, listResult: &pagination.PaginationResult{Total: 1}, getByIDsAccounts: []*Account{account}}
		result, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(t.Context(), &BulkUpdateAccountsInput{
			Filters: &BulkUpdateAccountFilters{Platform: PlatformOpenAI}, Extra: map[string]any{PrismBrowserEnabledKey: true, PrismBrowserModelsKey: []string{}},
		})
		require.Equal(t, []int64{7}, repo.getByIDsIDs)
		if compatible {
			require.NoError(t, err)
			require.Equal(t, 1, result.Success)
		} else {
			require.Error(t, err)
			require.Zero(t, repo.bulkUpdateCalls)
		}
	}
	repo := &accountRepoStubForBulkUpdate{getByIDsAccounts: []*Account{{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}}}
	_, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(t.Context(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1, 2}, Extra: map[string]any{PrismBrowserEnabledKey: true},
	})
	require.Error(t, err)
	require.Zero(t, repo.bulkUpdateCalls)
}

func TestBulkPrismOmittedConfigurationLeavesExistingFieldsUntouched(t *testing.T) {
	repo := &accountRepoStubForBulkUpdate{}
	_, err := (&adminServiceImpl{accountRepo: repo}).BulkUpdateAccounts(t.Context(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1}, Extra: map[string]any{"unrelated": true},
	})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"unrelated": true}, repo.lastBulkUpdate.Extra)
}
