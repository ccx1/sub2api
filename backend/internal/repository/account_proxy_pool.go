package repository

import (
	"context"
	"database/sql"
	"errors"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func NewAccountRepositoryWithProxyPool(client *dbent.Client, db *sql.DB, cache service.SchedulerCache, pool *ProxyPoolAllocator) service.AccountRepository {
	repo := newAccountRepositoryWithSQL(client, db, cache)
	repo.proxyPool = pool
	return repo
}

func NewAdminAccountRepositoryWithProxyPool(client *dbent.Client, db *sql.DB, cache service.SchedulerCache, pool *ProxyPoolAllocator) service.AdminAccountRepository {
	repo := newAccountRepositoryWithSQL(client, db, cache)
	repo.proxyPool = pool
	return repo
}

func (r *accountRepository) SelectBalancedProxy(ctx context.Context, selection service.ProxyPoolSelection) (*service.Proxy, error) {
	if r.proxyPool == nil {
		return nil, errors.New("proxy pool allocator is not configured")
	}
	return r.proxyPool.Select(ctx, selection)
}

func (r *accountRepository) ReportRandomProxyFailure(ctx context.Context, accountID, proxyID int64) error {
	return r.proxyPool.ReportFailure(ctx, accountID, proxyID)
}
