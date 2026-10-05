package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrismImportSettingsRoundTripAndCreation(t *testing.T) {
	settings := DefaultAccountImportSettings()
	settings.Enabled = true
	settings.Extra = map[string]any{PrismBrowserEnabledKey: true, PrismBrowserModelsKey: []string{" gpt-6.1-sol ", "gpt-6.1-sol"}}
	settingsService, _ := accountImportSettingsService(t, DefaultAccountImportSettings())
	saved, err := settingsService.UpdateAccountImportSettings(t.Context(), settings)
	require.NoError(t, err)
	reread, err := settingsService.GetAccountImportSettings(t.Context())
	require.NoError(t, err)
	require.Equal(t, saved, reread)
	require.Equal(t, []string{"gpt-6.1-sol"}, reread.Extra[PrismBrowserModelsKey])
	for _, extra := range []map[string]any{nil, {PrismBrowserEnabledKey: false}, {PrismBrowserEnabledKey: true, PrismBrowserModelsKey: []string{}}, {PrismBrowserModelsKey: []string{"gpt-6-luna"}}} {
		service, repo := accountImportDefaultTestService(t, *reread)
		input := accountImportDefaultTestInput()
		input.Type, input.Extra = AccountTypeOAuth, extra
		account, err := service.CreateAccount(t.Context(), input)
		require.NoError(t, err)
		require.Same(t, account, repo.created)
		require.Equal(t, extra == nil, account.IsPrismBrowserEnabledForModel("gpt-6.1-sol"))
		require.False(t, account.IsPrismBrowserEnabledForModel("gpt-6-luna"))
	}
	service, repo := accountImportDefaultTestService(t, *reread)
	account, err := service.CreateAccount(t.Context(), accountImportDefaultTestInput())
	require.NoError(t, err)
	require.Same(t, account, repo.created)
	require.NotContains(t, account.Extra, PrismBrowserEnabledKey, "setup-token accounts must not inherit Prism")
}

type prismExtraRepo struct {
	AccountRepository
	account *Account
	updates map[string]any
}

func (r *prismExtraRepo) GetByID(context.Context, int64) (*Account, error) { return r.account, nil }
func (r *prismExtraRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.updates = updates
	return nil
}

func TestPrismExtraUpdateDisablesScopeAndPreservesUnrelatedValues(t *testing.T) {
	repo := &prismExtraRepo{account: &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Extra: map[string]any{
		PrismBrowserEnabledKey: true, PrismBrowserModelsKey: []string{"gpt-6-luna"}, "other": true,
	}}}
	service := &adminServiceImpl{accountRepo: repo}
	require.NoError(t, service.UpdateAccountExtra(t.Context(), 1, map[string]any{PrismBrowserEnabledKey: false}))
	require.Equal(t, map[string]any{PrismBrowserEnabledKey: false, PrismBrowserModelsKey: []string{}}, repo.updates)
	require.Equal(t, true, repo.account.Extra["other"])
	repo.account.Type = AccountTypeAPIKey
	require.Error(t, service.UpdateAccountExtra(t.Context(), 1, map[string]any{PrismBrowserEnabledKey: true}))
}

func TestPrismSettingsValidateScopeAndAccount(t *testing.T) {
	for _, extra := range []map[string]any{
		{PrismBrowserEnabledKey: "true"}, {PrismBrowserEnabledKey: nil},
		{PrismBrowserEnabledKey: true, PrismBrowserModelsKey: nil},
		{PrismBrowserEnabledKey: true, PrismBrowserModelsKey: []any{123}},
		{PrismBrowserEnabledKey: true, PrismBrowserModelsKey: []string{"unsupported-model"}},
	} {
		require.Error(t, NormalizePrismBrowserExtra(extra))
	}
	extra := map[string]any{PrismBrowserEnabledKey: false, PrismBrowserModelsKey: []string{"gpt-6.1-sol"}, "other": true}
	require.NoError(t, NormalizePrismBrowserExtra(extra))
	require.Equal(t, []string{}, extra[PrismBrowserModelsKey])
	require.Equal(t, true, extra["other"])
	for _, account := range []*Account{
		{Platform: PlatformOpenAI, Type: AccountTypeAPIKey},
		{Platform: PlatformAnthropic, Type: AccountTypeOAuth},
		{Platform: PlatformOpenAI, Type: AccountTypeOAuth, ParentAccountID: new(int64(1))},
	} {
		account.Extra = map[string]any{PrismBrowserEnabledKey: true}
		require.Error(t, ValidatePrismBrowserAccount(account))
	}
}
