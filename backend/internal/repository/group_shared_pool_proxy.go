package repository

import (
	"context"
	"database/sql"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbproxy "github.com/Wei-Shaw/sub2api/ent/proxy"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func NewGroupRepositoryWithProxyHealth(client *dbent.Client, db *sql.DB, health service.ProxyLatencyCache) service.GroupRepository {
	repo := newGroupRepositoryWithSQL(client, db)
	repo.proxyPool = sharedPoolHealthReader(client, health)
	return repo
}

func NewAdminGroupRepositoryWithProxyHealth(client *dbent.Client, db *sql.DB, health service.ProxyLatencyCache) service.AdminGroupRepository {
	repo := newGroupRepositoryWithSQL(client, db)
	repo.proxyPool = sharedPoolHealthReader(client, health)
	return repo
}

func sharedPoolHealthReader(client *dbent.Client, health service.ProxyLatencyCache) *ProxyPoolAllocator {
	reader := &ProxyPoolAllocator{client: client, latencyCache: health}
	reader.loadCandidates = reader.readCandidates
	return reader
}

type sharedPoolProxyAvailability struct {
	fixed  map[int64]bool
	random map[int64]bool
	groups map[int64]int64
}

func (p *sharedPoolProxyAvailability) usable(account *service.Account) bool {
	if !account.IsRandomProxy() {
		return account.ProxyID == nil || p.fixed[*account.ProxyID]
	}
	if account.RandomProxyEmptyPoolPolicy() == service.RandomProxyEmptyPoolPolicyDirect {
		return true
	}
	if account.RandomProxyPoolScope() == service.RandomProxyPoolGroup {
		for id := range p.random {
			if groupID := p.groups[id]; groupID > 0 && groupID == account.RandomProxyGroupID() {
				return true
			}
		}
		return false
	}
	if account.RandomProxyPoolScope() != service.RandomProxyPoolSelected {
		return len(p.random) > 0
	}
	for _, id := range account.RandomProxyPoolIDs() {
		if p.random[id] {
			return true
		}
	}
	return false
}

// 仅查询健康快照，不分配随机代理租约，浏览共享池不会占用代理名额。
func (r *groupRepository) sharedPoolProxies(ctx context.Context) (*sharedPoolProxyAvailability, error) {
	items, err := r.client.Proxy.Query().Where(dbproxy.StatusEQ(service.StatusActive), dbproxy.DeletedAtIsNil(),
		dbproxy.Or(dbproxy.ExpiresAtIsNil(), dbproxy.ExpiresAtGT(time.Now()))).All(ctx)
	if err != nil {
		return nil, err
	}
	result := &sharedPoolProxyAvailability{fixed: make(map[int64]bool), random: make(map[int64]bool), groups: make(map[int64]int64)}
	for _, proxy := range items {
		result.fixed[proxy.ID] = true
		if proxy.GroupID != nil {
			result.groups[proxy.ID] = *proxy.GroupID
		}
	}
	if r.proxyPool == nil {
		for id := range result.fixed {
			result.random[id] = true
		}
		return result, nil
	}
	candidates, err := r.proxyPool.loadCandidates(ctx, service.ProxyPoolSelection{})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.proxy.ID)
	}
	health, err := r.proxyPool.latencyCache.GetProxyLatencies(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		if _, _, usable := proxyPoolQuality(candidate.proxy, health[candidate.proxy.ID]); usable {
			result.random[candidate.proxy.ID] = true
			if candidate.proxy.GroupID != nil {
				result.groups[candidate.proxy.ID] = *candidate.proxy.GroupID
			}
		}
	}
	return result, nil
}
