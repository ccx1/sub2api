package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProxyRegionAdminRejectsInvalidConfigurationWithoutWrites(t *testing.T) {
	for name, action := range map[string]func(*adminServiceImpl, map[string]any) error{
		"create": func(s *adminServiceImpl, extra map[string]any) error {
			_, err := s.CreateAccount(context.Background(), &CreateAccountInput{Name: "region", Platform: PlatformOpenAI, Type: AccountTypeOAuth, SkipDefaultGroupBind: true, Extra: extra})
			return err
		},
		"update": func(s *adminServiceImpl, extra map[string]any) error {
			_, err := s.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Extra: extra})
			return err
		},
		"patch": func(s *adminServiceImpl, extra map[string]any) error {
			return s.UpdateAccountExtra(context.Background(), 1, extra)
		},
		"bulk": func(s *adminServiceImpl, extra map[string]any) error {
			_, err := s.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Extra: extra})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			account := adminRandomProxyGroupAccount()
			account.Extra = map[string]any{ProxyRegionModeExtraKey: "manual", ProxyRegionCountryExtraKey: "JP"}
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: account}}
			svc := &adminServiceImpl{accountRepo: repo}
			err := action(svc, map[string]any{ProxyRegionModeExtraKey: "manual", ProxyRegionCountryExtraKey: "not-a-country"})
			require.Error(t, err)
			require.Empty(t, repo.bulkUpdates)
			require.Empty(t, repo.updates)
			require.Equal(t, "JP", repo.accounts[1].Extra[ProxyRegionCountryExtraKey])
		})
	}
}

func TestProxyRegionAdminPartialEditPreservesAndClearsRestriction(t *testing.T) {
	account := adminRandomProxyGroupAccount()
	account.Extra = map[string]any{ProxyRegionModeExtraKey: "manual", ProxyRegionCountryExtraKey: "JP"}
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: account}}
	svc := &adminServiceImpl{accountRepo: repo}
	updated, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Extra: map[string]any{"custom": true}})
	require.NoError(t, err)
	country, err := updated.ProxyRegionCountry()
	require.NoError(t, err)
	require.Equal(t, "JP", country)
	updated, err = svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Extra: map[string]any{ProxyRegionModeExtraKey: "off"}})
	require.NoError(t, err)
	country, err = updated.ProxyRegionCountry()
	require.NoError(t, err)
	require.Empty(t, country)
}
