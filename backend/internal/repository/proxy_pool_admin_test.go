package repository

import (
	"context"
	"database/sql"
	"strconv"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyPoolAdminBindingsSurviveLeaseAndFollowRotation(t *testing.T) {
	a, server := newProxyPoolAllocatorTest(t, 1, poolCandidate(1), poolCandidate(2))
	ctx := context.Background()
	first, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	server.FastForward(24 * time.Hour)
	bindings, err := readProxyPoolAdminBindings(ctx, a.rdb, []int64{1, 2})
	require.NoError(t, err)
	require.Equal(t, first.ID, bindings[7].proxyID)
	for range 3 {
		require.NoError(t, a.ReportFailure(ctx, 7, first.ID))
	}
	bindings, err = readProxyPoolAdminBindings(ctx, a.rdb, []int64{1, 2})
	require.NoError(t, err)
	require.Empty(t, bindings)
	second, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	bindings, err = readProxyPoolAdminBindings(ctx, a.rdb, []int64{1, 2})
	require.NoError(t, err)
	require.Equal(t, second.ID, bindings[7].proxyID)
	require.False(t, a.rdb.SIsMember(ctx, proxyPoolBoundAccountsKey(strconv.FormatInt(first.ID, 10)), "7").Val())
}

func TestProxyPoolAdminIgnoresStaleReverseBinding(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0, poolCandidate(1))
	ctx := context.Background()
	_, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	require.NoError(t, a.rdb.SAdd(ctx, proxyPoolBoundAccountsKey("2"), "7", "request:invalid").Err())
	bindings, err := readProxyPoolAdminBindings(ctx, a.rdb, []int64{1, 2})
	require.NoError(t, err)
	require.Len(t, bindings, 1)
	require.EqualValues(t, 1, bindings[7].proxyID)
}

func TestProxyPoolAdminEmptySelectedScopeReleasesBinding(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(1))
	ctx := context.Background()
	_, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7, Restricted: true})
	require.NoError(t, err)
	require.Nil(t, selected)
	bindings, err := readProxyPoolAdminBindings(ctx, a.rdb, []int64{1})
	require.NoError(t, err)
	require.Empty(t, bindings)
	require.Zero(t, a.rdb.ZCard(ctx, proxyPoolLeaseKey("1")).Val())
}

func newProxyPoolAdminTest(t *testing.T) (*proxyRepository, sqlmock.Sqlmock, *ProxyPoolAllocator) {
	t.Helper()
	a, _ := newProxyPoolAllocatorTest(t, 0, poolCandidate(1))
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := NewProxyRepositoryWithProxyPool(nil, db, a.rdb).(*proxyRepository)
	return repo, mock, a
}

func proxyPoolAdminProxyRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "protocol", "host", "port", "username", "password", "group_id"}).
		AddRow(1, "", "", 0, sql.NullString{}, sql.NullString{}, nil)
}

func proxyPoolAdminAccountRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "platform", "type", "notes", "extra", "proxy_id", "status"})
}

func TestProxyPoolAdminCountsCurrentDynamicAndFixedAccounts(t *testing.T) {
	r, mock, a := newProxyPoolAdminTest(t)
	ctx := context.Background()
	for id := int64(7); id <= 10; id++ {
		_, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: id})
		require.NoError(t, err)
	}
	mock.ExpectQuery("SELECT id, protocol, host, port, username, password, group_id FROM proxies").WillReturnRows(proxyPoolAdminProxyRows())
	mock.ExpectQuery("SELECT id, name, platform, type, notes, extra, proxy_id, status").WithArgs("{7,8,9,10}").
		WillReturnRows(proxyPoolAdminAccountRows().
			AddRow(7, "random", "openai", "oauth", nil, []byte(`{"proxy_mode":"random"}`), nil, service.StatusActive).
			AddRow(8, "changed_to_fixed", "openai", "oauth", nil, []byte(`{"proxy_mode":"fixed"}`), 1, service.StatusActive).
			AddRow(9, "disabled", "openai", "oauth", nil, []byte(`{"proxy_mode":"random"}`), nil, service.StatusDisabled).
			AddRow(10, "pool_changed", "openai", "oauth", nil, []byte(`{"proxy_mode":"random","random_proxy_pool_scope":"selected","random_proxy_pool_ids":[2]}`), nil, service.StatusActive))
	mock.ExpectQuery("SELECT id, proxy_id FROM accounts").WillReturnRows(sqlmock.NewRows([]string{"id", "proxy_id"}).AddRow(8, 1).AddRow(11, 1))
	counts, err := r.GetAccountCountsForProxies(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 3, counts[1])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyPoolAdminDetailCombinesFixedAndDynamicWithoutDuplicates(t *testing.T) {
	r, mock, a := newProxyPoolAdminTest(t)
	ctx := context.Background()
	_, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	mock.ExpectQuery("SELECT id, protocol, host, port, username, password, group_id FROM proxies").WithArgs(sqlmock.AnyArg(), "{1}").
		WillReturnRows(proxyPoolAdminProxyRows())
	mock.ExpectQuery("SELECT id, name, platform, type, notes, extra, proxy_id, status").WithArgs("{7}").
		WillReturnRows(proxyPoolAdminAccountRows().AddRow(7, "random", "openai", "oauth", nil, []byte(`{"proxy_mode":"random"}`), nil, service.StatusActive))
	mock.ExpectQuery("SELECT id, name, platform, type, notes").WithArgs(int64(1)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "name", "platform", "type", "notes"}).
			AddRow(7, "old_duplicate", "openai", "oauth", nil).AddRow(8, "fixed", "openai", "oauth", nil))
	accounts, err := r.ListAccountSummariesByProxyID(ctx, 1)
	require.NoError(t, err)
	require.Len(t, accounts, 2)
	require.EqualValues(t, 8, accounts[0].ID)
	require.Equal(t, "random", accounts[1].Name)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProxyPoolAdminDoesNotDisplayBindingFromOldProxyAddress(t *testing.T) {
	r, mock, a := newProxyPoolAdminTest(t)
	ctx := context.Background()
	_, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	mock.ExpectQuery("SELECT id, protocol, host, port, username, password, group_id FROM proxies").WillReturnRows(
		sqlmock.NewRows([]string{"id", "protocol", "host", "port", "username", "password", "group_id"}).AddRow(1, "http", "new.example", 8080, nil, nil, nil))
	mock.ExpectQuery("SELECT id, name, platform, type, notes, extra, proxy_id, status").WillReturnRows(
		proxyPoolAdminAccountRows().AddRow(7, "random", "openai", "oauth", nil, []byte(`{"proxy_mode":"random"}`), nil, service.StatusActive))
	accounts, err := r.dynamicProxyAccounts(ctx, []int64{1})
	require.NoError(t, err)
	require.Empty(t, accounts)
	require.NoError(t, mock.ExpectationsWereMet())
}
