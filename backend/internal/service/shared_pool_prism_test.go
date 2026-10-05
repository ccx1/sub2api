//go:build unit

package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedPrismCreateScopesAndProtectsOwner(t *testing.T) {
	svc, repo, input := sharedDailyCooldownFixture()
	input.Platform, input.Type = PlatformOpenAI, AccountTypeOAuth
	input.Credentials = map[string]any{"access_token": "fixture-only"}
	models := []string{"gpt-6.1-sol"}
	input.PrismBrowserEnabled, input.PrismBrowserModels = new(true), &models
	view, err := svc.Create(t.Context(), 7, input)
	require.NoError(t, err)
	require.True(t, *view.PrismBrowserEnabled)
	require.Equal(t, models, *view.PrismBrowserModels)
	require.True(t, repo.accounts.account.IsPrismBrowserEnabledForModel("gpt-6.1-sol"))
	require.False(t, repo.accounts.account.IsPrismBrowserEnabledForModel("gpt-6-luna"))
	require.Equal(t, int64(7), repo.accounts.account.Extra[SharedPoolOwnerKey])
	require.Equal(t, RandomProxyEmptyPoolPolicyReject, repo.accounts.account.RandomProxyEmptyPoolPolicy())
	input.Credentials = nil
	_, err = svc.Update(t.Context(), 8, 1, input)
	require.ErrorIs(t, err, ErrSharedPoolAccountNotFound)
	require.Zero(t, repo.updateCalls)
}

func TestSharedPrismUpdateOnlyWritesExplicitFamily(t *testing.T) {
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Extra: map[string]any{PrismBrowserEnabledKey: true, PrismBrowserModelsKey: []string{"gpt-6.1-sol"}}}
	svc, _ := newSharedTicketService(account)
	repo := svc.repo.(*sharedPoolRepoStub)
	input := SharedPoolAccountInput{Name: "mine", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 2}
	_, err := svc.Update(t.Context(), 7, 1, input)
	require.NoError(t, err)
	require.False(t, repo.lastUpdate.PrismBrowserChanged)
	input.PrismBrowserEnabled = new(false)
	_, err = svc.Update(t.Context(), 7, 1, input)
	require.NoError(t, err)
	require.True(t, repo.lastUpdate.PrismBrowserChanged)
	require.Equal(t, map[string]any{PrismBrowserEnabledKey: false, PrismBrowserModelsKey: []string{}}, repo.lastUpdate.PrismBrowserExtra)
}

func TestSharedPrismRejectsUnsupportedAccountsAndInvalidScope(t *testing.T) {
	for _, account := range []*Account{
		{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{Platform: PlatformGemini, Type: AccountTypeOAuth},
		{Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: new(int64(2))},
	} {
		_, err := sharedPrismBrowserExtra(SharedPoolAccountInput{PrismBrowserEnabled: new(true)}, account)
		require.Error(t, err)
	}
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	models := []string{"gpt-6-astra"}
	_, err := sharedPrismBrowserExtra(SharedPoolAccountInput{PrismBrowserEnabled: new(true), PrismBrowserModels: &models}, account)
	require.Error(t, err)
	_, err = sharedPrismBrowserExtra(SharedPoolAccountInput{PrismBrowserModels: &models}, account)
	require.Error(t, err)
}

func TestSharedPrismViewRetainsEmptyScopeAndHidesSecrets(t *testing.T) {
	account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "hidden-token"},
		Extra:       map[string]any{PrismBrowserEnabledKey: true, PrismBrowserModelsKey: []string{}, "private": "hidden-extra"}}
	view := sharedAccountView(account, SharedPoolAccountRecord{}, 0, 0, false)
	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"prism_browser_models":[]`)
	require.NotContains(t, string(encoded), "hidden-")
	delete(account.Extra, PrismBrowserModelsKey)
	view = sharedAccountView(account, SharedPoolAccountRecord{}, 0, 0, false)
	require.Equal(t, PrismBrowserSupportedModels(), *view.PrismBrowserModels)
}
