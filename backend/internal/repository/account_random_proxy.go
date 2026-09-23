package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbproxy "github.com/Wei-Shaw/sub2api/ent/proxy"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *accountRepository) ReportRandomProxySuccess(ctx context.Context, accountID, proxyID int64) error {
	return r.proxyPool.ReportSuccess(ctx, accountID, proxyID)
}

func (r *accountRepository) SelectRandomActiveProxyFromPool(ctx context.Context, ids []int64) (*service.Proxy, error) {
	return r.selectRandomActiveProxy(ctx, ids, true)
}

func (r *accountRepository) GetRandomProxyGroupIDs(ctx context.Context, groupID int64) ([]int64, error) {
	if groupID <= 0 {
		return nil, nil
	}
	if r == nil || r.client == nil {
		return nil, errors.New("proxy group database unavailable")
	}
	return r.client.Proxy.Query().Where(dbproxy.GroupIDEQ(groupID), dbproxy.DeletedAtIsNil(), excludePrivateSharedProxies).IDs(ctx)
}

func (r *accountRepository) selectRandomActiveProxy(ctx context.Context, ids []int64, restricted bool) (*service.Proxy, error) {
	if (restricted && len(ids) == 0) || r == nil || r.client == nil {
		return nil, nil
	}
	now := time.Now()
	query := r.client.Proxy.Query().Where(
		excludePrivateSharedProxies,
		dbproxy.StatusEQ(service.StatusActive), dbproxy.DeletedAtIsNil(),
		dbproxy.Or(dbproxy.ExpiresAtIsNil(), dbproxy.ExpiresAtGT(now)),
	)
	if restricted {
		query = query.Where(dbproxy.IDIn(ids...))
	}
	// 一次快照同时过滤和抽样，避免 COUNT 与 OFFSET 之间过期造成假空池。
	item, err := query.Order(func(selector *entsql.Selector) {
		selector.OrderExpr(entsql.Expr("RANDOM()"))
	}).First(ctx)
	if dbent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return proxyEntityToService(item), nil
}

// 出口观察只更新自己的 JSON 键，不改配置版本，也不触发调度桶重建。
func (r *accountRepository) RecordRandomProxyUsage(ctx context.Context, id int64, usage service.RandomProxyUsage) error {
	payload, err := json.Marshal(usage)
	if err != nil {
		return err
	}
	_, err = r.sql.ExecContext(ctx, `UPDATE accounts
SET extra = jsonb_set(COALESCE(extra, '{}'::jsonb), '{random_proxy_last_used}', $2::jsonb)
WHERE id = $1 AND deleted_at IS NULL AND extra->>'proxy_mode' = 'random'
AND COALESCE(extra #>> '{random_proxy_last_used,used_at}', '') < $3`, id, string(payload), usage.UsedAt)
	return err
}

// 与更新配置竞争时，以数据库当前策略和当前池为准，避免旧请求误禁用账号。
func (r *accountRepository) DisableRandomProxyAccountIfUnavailable(ctx context.Context, id int64) error {
	if r.proxyPool != nil {
		return r.disableAccountWhenBalancedPoolUnavailable(ctx, id)
	}
	current, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if current != nil {
		country, err := current.ProxyRegionCountry()
		if err != nil {
			return err
		}
		if country != "" {
			return errors.New("cannot disable a region-restricted account without the proxy pool allocator")
		}
	}
	_, err = r.sql.ExecContext(ctx, `WITH disabled AS (
 UPDATE accounts a SET status='disabled', schedulable=FALSE,
 error_message='random proxy pool has no active proxies', proxy_id=NULL, updated_at=NOW()
 WHERE a.id=$1 AND a.deleted_at IS NULL AND a.status <> 'disabled'
 AND lower(btrim(a.extra->>'proxy_mode'))='random'
 AND lower(btrim(a.extra->>'random_proxy_empty_pool_policy'))='disable'
 AND COALESCE(NULLIF(btrim(a.extra->>'proxy_region_mode'), ''), 'off')='off'
 AND NOT EXISTS (
  SELECT 1 FROM proxies p WHERE p.deleted_at IS NULL AND p.status='active'
  AND NOT EXISTS (SELECT 1 FROM shared_pool_proxies sp WHERE sp.proxy_id=p.id)
  AND (p.expires_at IS NULL OR p.expires_at>NOW())
  AND (lower(btrim(COALESCE(a.extra->>'random_proxy_pool_scope','all'))) NOT IN ('selected','group')
       OR (lower(btrim(a.extra->>'random_proxy_pool_scope'))='selected'
           AND a.extra->'random_proxy_pool_ids' @> jsonb_build_array(p.id))
       OR (lower(btrim(a.extra->>'random_proxy_pool_scope'))='group'
           AND p.group_id::text=a.extra->>'random_proxy_group_id'))
 ) RETURNING a.id
)
INSERT INTO scheduler_outbox (event_type,account_id,payload)
SELECT $2,id,'{}'::jsonb FROM disabled`, id, service.SchedulerOutboxEventAccountChanged)
	if err == nil {
		r.syncSchedulerAccountSnapshot(ctx, id)
	}
	return err
}
