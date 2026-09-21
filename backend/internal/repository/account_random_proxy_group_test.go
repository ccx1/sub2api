package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestRandomProxyGroupDatabaseMembershipDrivesAffinityAndEmptyRelease(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	a, _ := newProxyPoolAllocatorTest(t, 0)
	a.client, a.loadCandidates = client, a.readCandidates
	repo := newAccountRepositoryWithSQL(client, nil, nil)
	repo.proxyPool = a
	ctx := context.Background()
	group, err := client.ProxyGroup.Create().SetName("selected-group").Save(ctx)
	require.NoError(t, err)
	otherGroup, err := client.ProxyGroup.Create().SetName("other-group").Save(ctx)
	require.NoError(t, err)
	first := createPoolTestProxy(t, client, "group-first")
	second := createPoolTestProxy(t, client, "group-second")
	_, err = first.Update().SetGroupID(group.ID).Save(ctx)
	require.NoError(t, err)
	_, err = second.Update().SetGroupID(otherGroup.ID).Save(ctx)
	require.NoError(t, err)
	account := &service.Account{ID: 42, Extra: map[string]any{
		service.ProxyModeExtraKey:            service.ProxyModeRandom,
		service.RandomProxyPoolScopeExtraKey: service.RandomProxyPoolGroup,
		service.RandomProxyGroupIDExtraKey:   group.ID,
	}}
	require.NoError(t, service.ResolveRandomProxy(ctx, account, repo))
	require.Equal(t, first.ID, *account.ProxyID)
	_, err = first.Update().ClearGroupID().Save(ctx)
	require.NoError(t, err)
	_, err = second.Update().SetGroupID(group.ID).Save(ctx)
	require.NoError(t, err)
	require.NoError(t, service.ResolveRandomProxy(ctx, account, repo))
	require.Equal(t, second.ID, *account.ProxyID)
	_, err = second.Update().ClearGroupID().Save(ctx)
	require.NoError(t, err)
	require.ErrorIs(t, service.ResolveRandomProxy(ctx, account, repo), service.ErrRandomProxyUnavailable)
	require.Nil(t, account.ProxyID)
	require.Zero(t, a.rdb.Exists(ctx, proxyPoolAffinityKey("42")).Val())
	account.Extra[service.RandomProxyPoolScopeExtraKey] = service.RandomProxyPoolAll
	require.NoError(t, service.ResolveRandomProxy(ctx, account, repo))
	require.NotNil(t, account.ProxyID, "未分组代理仍属于全局池")
}

func TestProxyGroupAdminAndSharedAvailabilityStayWithinGroup(t *testing.T) {
	account := &service.Account{Status: service.StatusActive, Extra: map[string]any{
		service.ProxyModeExtraKey:            service.ProxyModeRandom,
		service.RandomProxyPoolScopeExtraKey: service.RandomProxyPoolGroup,
		service.RandomProxyGroupIDExtraKey:   int64(3),
	}}
	require.True(t, proxyPoolAdminAccountMatches(account, 7, 3))
	require.False(t, proxyPoolAdminAccountMatches(account, 7, 4))
	require.False(t, proxyPoolAdminAccountMatches(account, 7, 0))
	availability := &sharedPoolProxyAvailability{random: map[int64]bool{7: true}, groups: map[int64]int64{7: 3}}
	require.True(t, availability.usable(account))
	availability.groups[7] = 4
	require.False(t, availability.usable(account))
	account.Extra[service.RandomProxyEmptyPoolPolicyExtraKey] = service.RandomProxyEmptyPoolPolicyDirect
	require.True(t, availability.usable(account))
}

func TestRandomProxyGroupSurvivesSchedulerMetadataProjection(t *testing.T) {
	account := service.Account{ID: 42, Extra: map[string]any{
		service.ProxyModeExtraKey:            service.ProxyModeRandom,
		service.RandomProxyPoolScopeExtraKey: service.RandomProxyPoolGroup,
		service.RandomProxyGroupIDExtraKey:   int64(3),
	}}
	full, metadata, err := marshalSchedulerCacheAccount(account)
	require.NoError(t, err)
	for _, payload := range [][]byte{full, metadata} {
		var projected service.Account
		require.NoError(t, json.Unmarshal(payload, &projected))
		require.True(t, projected.IsRandomProxy())
		require.Equal(t, service.RandomProxyPoolGroup, projected.RandomProxyPoolScope())
		require.EqualValues(t, 3, projected.RandomProxyGroupID())
	}
}

func TestRandomProxyGroupBulkFixedProxyClearsStoredGroupRouting(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	proxyID := int64(7)
	mock.ExpectBegin()
	mock.ExpectExec(`(?s)UPDATE accounts SET proxy_id = \$1.*- 'random_proxy_group_id' - 'random_proxy_pool_scope' - 'random_proxy_pool_ids'`).
		WithArgs(proxyID, "{42}").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()
	count, err := repo.BulkUpdate(context.Background(), []int64{42}, service.AccountBulkUpdate{ProxyID: &proxyID})
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.NoError(t, mock.ExpectationsWereMet())
}
