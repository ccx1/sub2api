package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func expectTransportProxy(mock sqlmock.Sqlmock, proxy *service.Proxy) {
	mock.ExpectQuery(`SELECT .* FROM "proxies" WHERE .*`).WithArgs(proxy.ID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "status", "protocol", "host", "port"}).
			AddRow(proxy.ID, proxy.Status, proxy.Protocol, proxy.Host, proxy.Port))
}

func TestFixedProxyTransportCooldownRoutesToPoolWithoutDatabaseWrite(t *testing.T) {
	ctx := context.Background()
	old := &service.Proxy{ID: 70, Status: service.StatusActive, Protocol: "http", Host: "old.test", Port: 1333}
	next := &service.Proxy{ID: 58, Status: service.StatusActive, Protocol: "http", Host: "next.test", Port: 641}
	pool, server := newProxyPoolAllocatorTest(t, 0, proxyPoolCandidate{proxy: old}, proxyPoolCandidate{proxy: next})
	repo, mock := newCodexTicketCASRepo(t)
	repo.proxyPool = pool
	account := &service.Account{ID: 663, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "test"}, ProxyID: &old.ID, Proxy: old}
	now := time.Now().Truncate(time.Second)
	server.SetTime(now)
	require.NoError(t, repo.ReportProxyTransportFailure(ctx, account.ID, old))
	expectTransportProxy(mock, old)
	mock.ExpectQuery(`SELECT .* FROM "proxies" WHERE .*shared_pool_proxies`).WithArgs(old.ID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(old.ID))
	resolved := *account
	require.NoError(t, service.ResolveRandomProxyFromSource(ctx, &resolved, repo))
	require.Equal(t, next.ID, *resolved.ProxyID)
	require.Equal(t, old.ID, *account.ProxyID)
	require.Nil(t, resolved.Extra)
	// 发布仍核对固定配置70，同时锁住本次真正验证的58和原线路身份。
	mock.ExpectBegin()
	tx, err := repo.client.Tx(ctx)
	require.NoError(t, err)
	for _, proxy := range []*service.Proxy{next, old} {
		mock.ExpectQuery(`SELECT protocol, host, port, .*FROM proxies.*FOR SHARE`).WithArgs(proxy.ID).
			WillReturnRows(sqlmock.NewRows([]string{"protocol", "host", "port", "username", "password", "status"}).
				AddRow(proxy.Protocol, proxy.Host, proxy.Port, "", "", service.StatusActive))
	}
	expectCodexTicketCAS(mock).WithArgs("codex_turn_ticket:model", "null", account.ID, service.PlatformOpenAI,
		service.AccountTypeOAuth, `{"access_token":"test"}`, old.ID, "null", codexTicketCASDefaultConfig).
		WillReturnResult(sqlmock.NewResult(0, 1))
	changed, err := repo.CompareAndSwapCodexTicket(dbent.NewTxContext(ctx, tx), &resolved, "model", nil)
	require.NoError(t, err)
	require.True(t, changed)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	server.SetTime(now.Add(5 * time.Minute))
	expectTransportProxy(mock, old)
	resolved = *account
	require.NoError(t, service.ResolveRandomProxyFromSource(ctx, &resolved, repo))
	require.Equal(t, old.ID, *resolved.ProxyID)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestFixedPrivateProxyTransportCooldownCannotBorrowPublicPool(t *testing.T) {
	ctx := context.Background()
	proxy := &service.Proxy{ID: 70, Status: service.StatusActive, Protocol: "http", Host: "private.test", Port: 1333}
	pool, _ := newProxyPoolAllocatorTest(t, 0, poolCandidate(58))
	repo, mock := newCodexTicketCASRepo(t)
	repo.proxyPool = pool
	require.NoError(t, repo.ReportProxyTransportFailure(ctx, 663, proxy))
	expectTransportProxy(mock, proxy)
	mock.ExpectQuery(`SELECT .* FROM "proxies" WHERE .*shared_pool_proxies`).WithArgs(proxy.ID).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	_, err := repo.ResolveFixedProxyFailover(ctx, &service.Account{ID: 663, ProxyID: &proxy.ID, Proxy: proxy})
	require.ErrorContains(t, err, "private account proxy")
	require.Zero(t, pool.rdb.Exists(ctx, proxyPoolAffinityKey("663")).Val())
	require.NoError(t, mock.ExpectationsWereMet())
}
