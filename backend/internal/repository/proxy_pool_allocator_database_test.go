package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func createPoolTestProxy(t *testing.T, client *dbent.Client, name string) *dbent.Proxy {
	t.Helper()
	proxy, err := client.Proxy.Create().SetName(name).SetProtocol("http").SetHost("localhost").SetPort(8080).Save(context.Background())
	require.NoError(t, err)
	return proxy
}

func TestProxyPoolAllocatorDatabaseFiltersUnavailableAndHonorsSubpool(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	a, _ := newProxyPoolAllocatorTest(t, 0)
	a.client = client
	a.loadCandidates = a.readCandidates
	ctx := context.Background()
	active := createPoolTestProxy(t, client, "active")
	other := createPoolTestProxy(t, client, "other")
	expired := createPoolTestProxy(t, client, "expired")
	disabled := createPoolTestProxy(t, client, "disabled")
	deleted := createPoolTestProxy(t, client, "deleted")
	_, err := expired.Update().SetExpiresAt(time.Now().Add(-time.Hour)).Save(ctx)
	require.NoError(t, err)
	_, err = disabled.Update().SetStatus(service.StatusDisabled).Save(ctx)
	require.NoError(t, err)
	_, err = deleted.Update().SetDeletedAt(time.Now()).Save(ctx)
	require.NoError(t, err)
	all, err := a.readCandidates(ctx, service.ProxyPoolSelection{})
	require.NoError(t, err)
	var ids []int64
	for _, candidate := range all {
		ids = append(ids, candidate.proxy.ID)
	}
	require.ElementsMatch(t, []int64{active.ID, other.ID}, ids)
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 1, Restricted: true, IDs: []int64{active.ID, expired.ID, disabled.ID, deleted.ID}})
	require.NoError(t, err)
	require.NotNil(t, selected)
	require.Equal(t, active.ID, selected.ID)
	selected, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 2, Restricted: true, IDs: []int64{99999}})
	require.NoError(t, err)
	require.Nil(t, selected)
}

func TestProxyPoolAllocatorDatabaseCountsOnlyLiveFixedBindings(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	a, _ := newProxyPoolAllocatorTest(t, 2)
	a.client = client
	a.loadCandidates = a.readCandidates
	ctx := context.Background()
	proxy := createPoolTestProxy(t, client, "counted")
	var fixedID int64
	for _, mode := range []string{"fixed", " RaNdOm ", "deleted"} {
		id := mustCreateAPIKeyRepoAccount(t, ctx, client, mode)
		update := client.Account.UpdateOneID(id).SetProxyID(proxy.ID)
		if mode == "deleted" {
			update.SetDeletedAt(time.Now())
		} else {
			update.SetExtra(map[string]any{service.ProxyModeExtraKey: mode})
		}
		require.NoError(t, update.Exec(ctx))
		if mode == "fixed" {
			fixedID = id
		}
	}
	fixed, err := a.readFixedAccounts(ctx, []int64{proxy.ID})
	require.NoError(t, err)
	require.Equal(t, []string{strconv.FormatInt(fixedID, 10)}, fixed[proxy.ID])
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: fixedID})
	require.NoError(t, err)
	require.NotNil(t, selected)
	selected, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 900})
	require.NoError(t, err)
	require.NotNil(t, selected, "fixed and dynamic copies of the same account count once")
	selected, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: 901})
	require.NoError(t, err)
	require.Nil(t, selected)
}
