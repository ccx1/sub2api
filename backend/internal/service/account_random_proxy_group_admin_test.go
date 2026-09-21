package service

import (
	"context"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"
)

func adminRandomProxyGroupAccount() *Account {
	return &Account{ID: 1, Name: "group-account", Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
		Status: StatusActive, Credentials: map[string]any{"api_key": "test"}, Extra: map[string]any{
			ProxyModeExtraKey: ProxyModeRandom, RandomProxyPoolScopeExtraKey: RandomProxyPoolGroup, RandomProxyGroupIDExtraKey: int64(1),
		}}
}

func TestRandomProxyGroupAdminRejectsMissingGroupBeforeWrites(t *testing.T) {
	actions := map[string]func(context.Context, *adminServiceImpl) error{
		"create": func(ctx context.Context, s *adminServiceImpl) error {
			_, err := s.CreateAccount(ctx, &CreateAccountInput{Name: "missing-group", Platform: PlatformAnthropic,
				Type: AccountTypeAPIKey, SkipDefaultGroupBind: true, Extra: map[string]any{
					ProxyModeExtraKey: ProxyModeRandom, RandomProxyPoolScopeExtraKey: RandomProxyPoolGroup, RandomProxyGroupIDExtraKey: int64(9),
				}})
			return err
		},
		"update partial group ID": func(ctx context.Context, s *adminServiceImpl) error {
			_, err := s.UpdateAccount(ctx, 1, &UpdateAccountInput{Extra: map[string]any{RandomProxyGroupIDExtraKey: int64(9)}})
			return err
		},
		"bulk partial group ID": func(ctx context.Context, s *adminServiceImpl) error {
			_, err := s.BulkUpdateAccounts(ctx, &BulkUpdateAccountsInput{AccountIDs: []int64{1}, Extra: map[string]any{RandomProxyGroupIDExtraKey: int64(9)}})
			return err
		},
		"patch partial group ID": func(ctx context.Context, s *adminServiceImpl) error {
			return s.UpdateAccountExtra(ctx, 1, map[string]any{RandomProxyGroupIDExtraKey: int64(9)})
		},
	}
	for name, action := range actions {
		t.Run(name, func(t *testing.T) {
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: adminRandomProxyGroupAccount()}}
			svc := &adminServiceImpl{accountRepo: repo, proxyRepo: &proxyGroupRepoStub{}}
			require.ErrorIs(t, action(context.Background(), svc), ErrProxyGroupNotFound)
			require.Len(t, repo.accounts, 1)
			require.EqualValues(t, 1, repo.accounts[1].RandomProxyGroupID())
			require.Empty(t, repo.bulkUpdates)
			require.Empty(t, repo.updates)
		})
	}
}

func TestRandomProxyGroupAdminPartialExtraPreservesGroupAndCanDisable(t *testing.T) {
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: adminRandomProxyGroupAccount()}}
	svc := &adminServiceImpl{accountRepo: repo, proxyRepo: &proxyGroupRepoStub{}}
	updated, err := svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Extra: map[string]any{"custom": true}})
	require.NoError(t, err)
	require.True(t, updated.IsRandomProxy())
	require.EqualValues(t, 1, updated.RandomProxyGroupID())
	require.Equal(t, RandomProxyPoolGroup, updated.RandomProxyPoolScope())
	svc.proxyRepo = nil
	updated, err = svc.UpdateAccount(context.Background(), 1, &UpdateAccountInput{Extra: map[string]any{ProxyModeExtraKey: ""}})
	require.NoError(t, err)
	require.False(t, updated.IsRandomProxy())
	require.NotContains(t, updated.Extra, RandomProxyGroupIDExtraKey)
	require.NotContains(t, updated.Extra, RandomProxyPoolScopeExtraKey)
}

func TestRandomProxyGroupValidationLeavesFixedGlobalAndSelectedIndependent(t *testing.T) {
	svc := &adminServiceImpl{}
	for _, extra := range []map[string]any{
		{ProxyModeExtraKey: "fixed", RandomProxyPoolScopeExtraKey: RandomProxyPoolGroup, RandomProxyGroupIDExtraKey: int64(9)},
		{ProxyModeExtraKey: ProxyModeRandom, RandomProxyPoolScopeExtraKey: RandomProxyPoolAll},
		{ProxyModeExtraKey: ProxyModeRandom, RandomProxyPoolScopeExtraKey: RandomProxyPoolSelected, RandomProxyPoolIDsExtraKey: []int64{7}},
	} {
		require.NoError(t, svc.validateAccountRandomProxyGroup(context.Background(), &Account{Extra: extra}))
	}
}

func TestRandomProxyGroupBulkDisableExplicitlyClearsMergedRouting(t *testing.T) {
	account := adminRandomProxyGroupAccount()
	repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{1: account}}
	svc := &adminServiceImpl{accountRepo: repo}
	_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{
		AccountIDs: []int64{1}, Extra: map[string]any{ProxyModeExtraKey: ""},
	})
	require.NoError(t, err)
	require.Len(t, repo.bulkUpdates, 1)
	merged := maps.Clone(account.Extra)
	maps.Copy(merged, repo.bulkUpdates[0].Extra)
	require.False(t, (&Account{Extra: merged}).IsRandomProxy())
	require.Equal(t, RandomProxyPoolAll, (&Account{Extra: merged}).RandomProxyPoolScope())
	require.Zero(t, (&Account{Extra: merged}).RandomProxyGroupID())
}
