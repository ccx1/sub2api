package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolGroupPersistenceAndAuthProjection(t *testing.T) {
	keys, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	repo := newGroupRepositoryWithSQL(client, nil)
	group := &service.Group{Name: "shared", Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, IsSharedPool: true}
	require.NoError(t, repo.Create(ctx, group))
	loaded, err := repo.GetByIDLite(ctx, group.ID)
	require.NoError(t, err)
	require.True(t, loaded.IsSharedPool)
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "shared-group-test@example.com")
	key := &service.APIKey{UserID: user.ID, Name: "shared", Key: "shared-group-test-key", GroupID: &group.ID, Status: service.StatusActive}
	require.NoError(t, keys.Create(ctx, key))
	for _, enabled := range []bool{true, false, true} {
		loaded.IsSharedPool = enabled
		require.NoError(t, repo.Update(ctx, loaded))
		actual, err := keys.GetByKeyForAuth(ctx, key.Key)
		require.NoError(t, err)
		require.Equal(t, enabled, actual.Group.IsSharedPool)
	}
}

func TestSharedPoolAvailabilityHydratesSchedulingState(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	repo := newGroupRepositoryWithSQL(client, db)
	g, err := client.Group.Create().SetName("shared-available").SetPlatform(service.PlatformOpenAI).SetIsSharedPool(true).Save(ctx)
	require.NoError(t, err)
	a, err := client.Account.Create().SetName("shared-account").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeOAuth).SetCredentials(map[string]any{}).SetSchedulable(true).Save(ctx)
	require.NoError(t, err)
	for _, limited := range []bool{false, true, false} {
		update := client.Account.UpdateOneID(a.ID)
		if limited {
			update.SetRateLimitResetAt(time.Now().Add(time.Hour))
		} else {
			update.ClearRateLimitResetAt()
		}
		require.NoError(t, update.Exec(ctx))
		mock.ExpectQuery("SELECT ag.account_id, ag.group_id").WithArgs(sqlmock.AnyArg(), service.StatusActive).
			WillReturnRows(sqlmock.NewRows([]string{"account_id", "group_id"}).AddRow(a.ID, g.ID))
		available, err := repo.SharedPoolAvailableGroupIDs(ctx, []int64{g.ID})
		require.NoError(t, err)
		require.Equal(t, !limited, available[g.ID])
	}
	mock.ExpectQuery("SELECT ag.account_id, ag.group_id").WithArgs(sqlmock.AnyArg(), service.StatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "group_id"}).AddRow(a.ID, g.ID))
	counts, err := repo.SharedPoolAvailableAccountCounts(ctx, []int64{g.ID})
	require.NoError(t, err)
	require.EqualValues(t, 1, counts[g.ID])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolProxyAvailabilityRespectsRandomScopeAndEmptyPolicy(t *testing.T) {
	state := &sharedPoolProxyAvailability{fixed: map[int64]bool{4: true}, random: map[int64]bool{7: true}}
	acc := &service.Account{Extra: map[string]any{service.ProxyModeExtraKey: service.ProxyModeRandom}}
	require.True(t, state.usable(acc))
	acc.Extra[service.RandomProxyPoolScopeExtraKey] = service.RandomProxyPoolSelected
	acc.Extra[service.RandomProxyPoolIDsExtraKey] = []int64{4}
	require.False(t, state.usable(acc), "private fixed proxy must not enter the platform random pool")
	acc.Extra[service.RandomProxyPoolIDsExtraKey] = []int64{7}
	require.True(t, state.usable(acc))
	state.random = map[int64]bool{}
	for _, policy := range []string{service.RandomProxyEmptyPoolPolicyReject, service.RandomProxyEmptyPoolPolicyDisable, service.RandomProxyEmptyPoolPolicyDirect} {
		acc.Extra[service.RandomProxyEmptyPoolPolicyExtraKey] = policy
		require.Equal(t, policy == service.RandomProxyEmptyPoolPolicyDirect, state.usable(acc))
	}
	acc.Extra = nil
	id := int64(4)
	acc.ProxyID = &id
	require.True(t, state.usable(acc))
	delete(state.fixed, id)
	require.False(t, state.usable(acc))
}

func TestSharedPoolProxyVisibilityReusesAllocatorHealthWithoutLeases(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	pool, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(7))
	repo := newGroupRepositoryWithSQL(client, nil)
	repo.proxyPool = pool
	ctx := context.Background()
	account := &service.Account{ID: 11, Extra: map[string]any{service.ProxyModeExtraKey: service.ProxyModeRandom}}
	for _, healthy := range []bool{true, false, true} {
		require.NoError(t, pool.latencyCache.SetProxyLatency(ctx, 7, &service.ProxyLatencyInfo{Success: healthy, UpdatedAt: time.Now()}))
		state, err := repo.sharedPoolProxies(ctx)
		require.NoError(t, err)
		require.Equal(t, healthy, state.usable(account))
		require.Zero(t, pool.rdb.ZCard(ctx, proxyPoolLeaseKey("7")).Val())
	}
}
