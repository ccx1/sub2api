//go:build sharedpoolintegration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func sharedManualTestGroup(t *testing.T, f sharedAccountPG, name, kind string, exclusive bool) int64 {
	t.Helper()
	group, err := f.client.Group.Create().SetName(name).SetPlatform(service.PlatformOpenAI).
		SetSubscriptionType(kind).SetIsExclusive(exclusive).SetIsSharedPool(false).Save(context.Background())
	require.NoError(t, err)
	return group.ID
}

func sharedManualBindings(t *testing.T, f sharedAccountPG, accountID int64) map[int64]int {
	t.Helper()
	rows, err := f.db.Query(`SELECT group_id,priority FROM account_groups WHERE account_id=$1 ORDER BY group_id`, accountID)
	require.NoError(t, err)
	defer rows.Close()
	result := make(map[int64]int)
	for rows.Next() {
		var id int64
		var priority int
		require.NoError(t, rows.Scan(&id, &priority))
		result[id] = priority
	}
	require.NoError(t, rows.Err())
	return result
}

func TestSharedManualGroupsPostgresMixedGroupsKeepPrioritiesAndOtherAccounts(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	a, other := f.account("manual-groups", f.a), f.account("other-groups", f.b)
	a.Extra[service.SharedPoolDispatchConsentKey], a.Priority = true, 31
	require.NoError(t, f.repo.CreateSharedAccount(ctx, a, f.owner, "manual-groups"))
	require.NoError(t, f.repo.CreateSharedAccount(ctx, other, f.owner, "other-groups"))
	_, err := f.db.Exec(`UPDATE account_groups SET priority=9 WHERE account_id=$1`, a.ID)
	require.NoError(t, err)
	otherBefore := sharedManualBindings(t, f, other.ID)
	ids := []int64{f.a,
		sharedManualTestGroup(t, f, "normal", service.SubscriptionTypeStandard, false),
		sharedManualTestGroup(t, f, "subscription", service.SubscriptionTypeSubscription, false),
		sharedManualTestGroup(t, f, "exclusive", service.SubscriptionTypeStandard, true),
		sharedManualTestGroup(t, f, "exclusive subscription", service.SubscriptionTypeSubscription, true),
	}
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{GroupIDs: &ids}))
	expected := map[int64]int{f.a: 9}
	for _, id := range ids[1:] {
		expected[id] = 31
	}
	require.Equal(t, expected, sharedManualBindings(t, f, a.ID))
	require.Equal(t, otherBefore, sharedManualBindings(t, f, other.ID))
	stored, err := f.client.Group.Get(ctx, ids[4])
	require.NoError(t, err)
	require.True(t, stored.IsExclusive)
	require.Equal(t, service.SubscriptionTypeSubscription, stored.SubscriptionType)
	require.False(t, stored.IsSharedPool, "assignment must not change consumer authorization flags")
	require.NoError(t, f.repo.accounts.BindGroups(ctx, a.ID, ids[1:]))
	newExclusive := sharedManualTestGroup(t, f, "generic add", service.SubscriptionTypeStandard, true)
	require.NoError(t, f.repo.accounts.AddToGroup(ctx, a.ID, newExclusive, 7))
	newSubscription := sharedManualTestGroup(t, f, "generic group", service.SubscriptionTypeSubscription, true)
	groupRepo := newGroupRepositoryWithSQL(f.client, f.db)
	require.NoError(t, groupRepo.BindAccountsToGroup(ctx, newSubscription, []int64{a.ID}))
	require.Len(t, sharedManualBindings(t, f, a.ID), 6)
	require.Equal(t, otherBefore, sharedManualBindings(t, f, other.ID))
}

func TestSharedManualGroupsPostgresRejectedSelectionRollsBackAllBindings(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	a := f.account("manual-reject", f.a)
	a.Extra[service.SharedPoolDispatchConsentKey] = true
	require.NoError(t, f.repo.CreateSharedAccount(ctx, a, f.owner, "manual-reject"))
	valid := sharedManualTestGroup(t, f, "valid sub", service.SubscriptionTypeSubscription, true)
	foreign := sharedManualTestGroup(t, f, "foreign", service.SubscriptionTypeStandard, false)
	inactive := sharedManualTestGroup(t, f, "inactive", service.SubscriptionTypeSubscription, false)
	deleted := sharedManualTestGroup(t, f, "deleted", service.SubscriptionTypeStandard, true)
	require.NoError(t, f.client.Group.UpdateOneID(foreign).SetPlatform(service.PlatformAnthropic).Exec(ctx))
	require.NoError(t, f.client.Group.UpdateOneID(inactive).SetStatus(service.StatusDisabled).Exec(ctx))
	require.NoError(t, f.client.Group.UpdateOneID(deleted).SetDeletedAt(time.Now()).Exec(ctx))
	before := sharedManualBindings(t, f, a.ID)
	for _, invalid := range []int64{foreign, inactive, deleted, 999999999} {
		ids := []int64{valid, invalid}
		require.Error(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{GroupIDs: &ids, Priority: new(2)}))
		require.Equal(t, before, sharedManualBindings(t, f, a.ID))
		stored, err := f.client.Account.Get(ctx, a.ID)
		require.NoError(t, err)
		require.Equal(t, a.Priority, stored.Priority, "invalid groups must also roll back account priority")
		require.Error(t, f.repo.accounts.BindGroups(ctx, a.ID, ids))
		require.Equal(t, before, sharedManualBindings(t, f, a.ID))
	}
}

func TestSharedManualGroupsPostgresLegacyAndOAuthRestrictionsRemain(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	standard := sharedManualTestGroup(t, f, "ordinary", service.SubscriptionTypeStandard, false)
	subscription := sharedManualTestGroup(t, f, "subscription", service.SubscriptionTypeSubscription, false)
	exclusive := sharedManualTestGroup(t, f, "exclusive", service.SubscriptionTypeStandard, true)
	oauthOnly := sharedManualTestGroup(t, f, "oauth only", service.SubscriptionTypeSubscription, true)
	require.NoError(t, f.client.Group.UpdateOneID(oauthOnly).SetRequireOauthOnly(true).Exec(ctx))
	for _, explicitFalse := range []bool{false, true} {
		a := f.account(fmt.Sprintf("legacy-%v", explicitFalse), f.a)
		if explicitFalse {
			a.Extra[service.SharedPoolDispatchConsentKey] = false
		}
		require.NoError(t, f.repo.CreateSharedAccount(ctx, a, f.owner, a.Name))
		before := sharedManualBindings(t, f, a.ID)
		for _, id := range []int64{standard, subscription, exclusive} {
			ids := []int64{id}
			require.Error(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{GroupIDs: &ids}))
			require.Error(t, f.repo.accounts.BindGroups(ctx, a.ID, ids))
			require.Equal(t, before, sharedManualBindings(t, f, a.ID))
		}
		ids := []int64{f.a, f.b}
		require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{GroupIDs: &ids}))
	}
	api := f.account("apikey", f.a)
	api.Type, api.Extra[service.SharedPoolDispatchConsentKey] = service.AccountTypeAPIKey, true
	require.NoError(t, f.repo.CreateSharedAccount(ctx, api, f.owner, api.Name))
	before := sharedManualBindings(t, f, api.ID)
	ids := []int64{subscription, oauthOnly}
	require.Error(t, f.repo.SetSharedAccountState(ctx, api.ID, service.SharedPoolAccountState{GroupIDs: &ids}))
	require.Error(t, f.repo.accounts.BindGroups(ctx, api.ID, ids))
	require.Equal(t, before, sharedManualBindings(t, f, api.ID))
}

func TestSharedManualGroupsPostgresAutomaticAssignmentRemainsStrict(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		kind      string
		exclusive bool
	}{
		{service.SubscriptionTypeSubscription, false}, {service.SubscriptionTypeStandard, true}, {service.SubscriptionTypeSubscription, true},
	} {
		name := fmt.Sprintf("auto-%s-%v", tc.kind, tc.exclusive)
		id := sharedManualTestGroup(t, f, name, tc.kind, tc.exclusive)
		a := f.account(name, id)
		a.Extra[service.SharedPoolDispatchConsentKey] = true
		require.Error(t, f.repo.CreateSharedAccount(ctx, a, f.owner, name+"-create"))
		a = f.account(name)
		a.Extra[service.SharedPoolDispatchConsentKey] = true
		require.NoError(t, f.repo.CreateSharedAccount(ctx, a, f.owner, name+"-pending"))
		require.Error(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{DefaultGroupIDs: []int64{id}, Enabled: new(true)}))
		ids := []int64{id}
		require.Error(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{OwnerID: f.owner, GroupIDs: &ids, Enabled: new(true)}))
		require.Empty(t, sharedManualBindings(t, f, a.ID))
	}
}
