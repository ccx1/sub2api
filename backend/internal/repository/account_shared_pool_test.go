package repository

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolSchedulerProjectionRetainsOwnershipAndSwitches(t *testing.T) {
	account := &service.Account{Platform: service.PlatformOpenAI, Status: service.StatusActive, Schedulable: true,
		Extra: filterSchedulerExtra(map[string]any{service.SharedPoolOwnerKey: int64(7), service.SharedPoolEnabledKey: false,
			service.SharedPoolAdminDisabledKey: false, service.SharedPoolSubscriptionTierKey: "pro", "unrelated": true})}
	require.EqualValues(t, 7, account.Extra[service.SharedPoolOwnerKey])
	require.Equal(t, "pro", account.Extra[service.SharedPoolSubscriptionTierKey])
	require.NotContains(t, account.Extra, "unrelated")
	require.False(t, account.IsSchedulable())
}

func TestSharedPoolLockedAccountUpdateKeepsCommittedOwnerAndSwitches(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	mock.ExpectQuery("SELECT extra FROM accounts").WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"extra"}).
		AddRow([]byte(`{"shared_pool_owner_id":9,"shared_pool_enabled":false,"shared_pool_admin_disabled":true,"shared_pool_subscription_tier":"pro"}`)))
	account := &service.Account{ID: 7, Extra: map[string]any{service.SharedPoolOwnerKey: 999, service.SharedPoolEnabledKey: true}}
	require.NoError(t, preserveLockedAccountProtection(context.Background(), client, account))
	require.Equal(t, float64(9), account.Extra[service.SharedPoolOwnerKey])
	require.Equal(t, false, account.Extra[service.SharedPoolEnabledKey])
	require.Equal(t, true, account.Extra[service.SharedPoolAdminDisabledKey])
	require.Equal(t, "pro", account.Extra[service.SharedPoolSubscriptionTierKey])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolPrivateProxyNeverEntersRandomOrBalancedPool(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	public := createPoolTestProxy(t, client, "public-shared-test")
	private := createPoolTestProxy(t, client, "private-shared-test")
	_, err := client.ExecContext(ctx, "INSERT INTO shared_pool_proxies(proxy_id,owner_user_id,fingerprint) VALUES(?,?,?)", private.ID, 7, "private-test")
	require.NoError(t, err)
	repo := newAccountRepositoryWithSQL(client, nil, nil)
	selected, err := repo.SelectRandomActiveProxyFromPool(ctx, []int64{private.ID})
	require.NoError(t, err)
	require.Nil(t, selected)
	selected, err = repo.SelectRandomActiveProxy(ctx)
	require.NoError(t, err)
	require.Equal(t, public.ID, selected.ID)
	pool, _ := newProxyPoolAllocatorTest(t, 0)
	pool.client = client
	candidates, err := pool.readCandidates(ctx, service.ProxyPoolSelection{})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, public.ID, candidates[0].proxy.ID)
	require.NoError(t, public.Update().SetExpiresAt(time.Now().Add(-time.Hour)).Exec(ctx))
	selected, err = repo.SelectRandomActiveProxy(ctx)
	require.NoError(t, err)
	require.Nil(t, selected)
}

func TestSharedPoolAccountFilterUsesPersistentRegistration(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	_, err := client.ExecContext(ctx, "CREATE TABLE shared_pool_accounts (account_id INTEGER PRIMARY KEY)")
	require.NoError(t, err)
	shared := mustCreateAPIKeyRepoAccount(t, ctx, client, "shared-filter")
	platform := mustCreateAPIKeyRepoAccount(t, ctx, client, "platform-filter")
	_, err = client.ExecContext(ctx, "INSERT INTO shared_pool_accounts(account_id) VALUES(?)", shared)
	require.NoError(t, err)
	for filter, expected := range map[string][]int64{"shared": {shared}, "platform": {platform}, "all": {shared, platform}} {
		ids, err := filterSharedAccounts(service.WithSharedAccountFilter(ctx, filter), client.Account.Query()).IDs(ctx)
		require.NoError(t, err)
		require.ElementsMatch(t, expected, ids)
	}
}

func TestSharedPoolAccountGroupCompatibility(t *testing.T) {
	group := &service.Group{Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard, Status: service.StatusActive, IsSharedPool: true}
	require.True(t, sharedAccountGroupAllowed(group, service.PlatformOpenAI, service.AccountTypeOAuth, false))
	group.IsSharedPool = false
	require.False(t, sharedAccountGroupAllowed(group, service.PlatformOpenAI, service.AccountTypeOAuth, false))
	require.True(t, sharedAccountGroupAllowed(group, service.PlatformOpenAI, service.AccountTypeOAuth, true))
	group.IsSharedPool = true
	require.False(t, sharedAccountGroupAllowed(group, service.PlatformAnthropic, service.AccountTypeOAuth, true))
	group.RequireOAuthOnly = true
	require.False(t, sharedAccountGroupAllowed(group, service.PlatformOpenAI, service.AccountTypeAPIKey, true))
	group.IsExclusive = true
	require.True(t, sharedAccountGroupAllowed(group, service.PlatformOpenAI, service.AccountTypeOAuth, true))
	require.False(t, sharedAccountDefaultGroupAllowed(group, service.PlatformOpenAI, service.AccountTypeOAuth, true))
}

func TestSharedPoolManualGroupCompatibilityPreservesAutomaticAndLegacyScopes(t *testing.T) {
	for _, tc := range []struct {
		name, kind                                   string
		exclusive, shared, modern, legacy, automatic bool
	}{
		{"standard", service.SubscriptionTypeStandard, false, false, true, false, true},
		{"subscription", service.SubscriptionTypeSubscription, false, false, true, false, false},
		{"exclusive standard", service.SubscriptionTypeStandard, true, false, true, false, false},
		{"exclusive subscription", service.SubscriptionTypeSubscription, true, false, true, false, false},
		{"legacy shared standard", service.SubscriptionTypeStandard, false, true, true, true, true},
		{"legacy shared subscription", service.SubscriptionTypeSubscription, false, true, true, false, false},
		{"legacy shared exclusive", service.SubscriptionTypeStandard, true, true, true, false, false},
		{"unknown billing type", "unknown", false, false, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			group := &service.Group{Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: tc.kind, IsExclusive: tc.exclusive, IsSharedPool: tc.shared}
			require.Equal(t, tc.modern, sharedAccountGroupAllowed(group, service.PlatformOpenAI, service.AccountTypeOAuth, true))
			require.Equal(t, tc.legacy, sharedAccountGroupAllowed(group, service.PlatformOpenAI, service.AccountTypeOAuth, false))
			require.Equal(t, tc.automatic, sharedAccountDefaultGroupAllowed(group, service.PlatformOpenAI, service.AccountTypeOAuth, true))
			group.RequireOAuthOnly = true
			require.False(t, sharedAccountGroupAllowed(group, service.PlatformOpenAI, service.AccountTypeAPIKey, true))
			group.RequireOAuthOnly = false
			require.False(t, sharedAccountGroupAllowed(group, service.PlatformGemini, service.AccountTypeOAuth, true))
			group.Status = service.StatusDisabled
			require.False(t, sharedAccountGroupAllowed(group, service.PlatformOpenAI, service.AccountTypeOAuth, true))
		})
	}
	composite := &service.Group{Platform: service.PlatformComposite, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard}
	require.False(t, sharedAccountGroupAllowed(composite, service.PlatformComposite, service.AccountTypeOAuth, true))
}

func TestSharedPoolOrdinaryCreateStripsForgedOwnership(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	repo := newAccountRepositoryWithSQL(client, nil, nil)
	account := &service.Account{Name: "forged-owner", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Status: service.StatusActive,
		Extra: map[string]any{service.SharedPoolOwnerKey: 7, service.SharedPoolEnabledKey: true, service.SharedPoolDispatchConsentKey: true}}
	require.NoError(t, repo.Create(context.Background(), account))
	row, err := client.Account.Get(context.Background(), account.ID)
	require.NoError(t, err)
	require.NotContains(t, row.Extra, service.SharedPoolOwnerKey)
	require.NotContains(t, row.Extra, service.SharedPoolEnabledKey)
	require.NotContains(t, row.Extra, service.SharedPoolDispatchConsentKey)
}

func TestSharedPoolAccountsExcludedFromUngroupedCandidates(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	shared := mustCreateAPIKeyRepoAccount(t, ctx, client, "ungrouped-shared")
	platform := mustCreateAPIKeyRepoAccount(t, ctx, client, "ungrouped-platform")
	require.NoError(t, client.Account.UpdateOneID(shared).SetExtra(map[string]any{service.SharedPoolOwnerKey: 7, service.SharedPoolEnabledKey: true}).Exec(ctx))
	ids, err := client.Account.Query().Where(excludeSharedAccount).IDs(ctx)
	require.NoError(t, err)
	require.Equal(t, []int64{platform}, ids)
}
