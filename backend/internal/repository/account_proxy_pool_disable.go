package repository

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *accountRepository) disableAccountWhenBalancedPoolUnavailable(ctx context.Context, id int64) error {
	current, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if current == nil || !current.IsRandomProxy() || current.RandomProxyEmptyPoolPolicy() != service.RandomProxyEmptyPoolPolicyDisable {
		return nil
	}
	selection, err := service.ResolveAccountProxyPoolSelection(ctx, current, r)
	if err != nil {
		return err
	}
	proxy, err := r.proxyPool.Select(ctx, selection)
	if err != nil || proxy != nil {
		return err
	}
	payload, err := json.Marshal(current.Extra)
	if err != nil {
		return err
	}
	billing, err := json.Marshal(map[string]any{
		"billing_currency": current.Credentials["billing_currency"],
		"price_country":    current.Credentials["price_country"],
	})
	if err != nil {
		return err
	}
	// 健康度和容量在分配器复核，写入时防止并发编辑后的新策略被旧请求禁用。
	_, err = r.sql.ExecContext(ctx, disableBalancedProxyAccountSQL, id, service.SchedulerOutboxEventAccountChanged, string(payload), string(billing))
	if err == nil {
		r.syncSchedulerAccountSnapshot(ctx, id)
	}
	return err
}

const disableBalancedProxyAccountSQL = `WITH disabled AS (
 UPDATE accounts a SET status='disabled', schedulable=FALSE,
 error_message='random proxy pool has no active proxies', proxy_id=NULL, updated_at=NOW()
 WHERE a.id=$1 AND a.deleted_at IS NULL AND a.status <> 'disabled'
 AND a.extra->'proxy_mode' IS NOT DISTINCT FROM $3::jsonb->'proxy_mode'
 AND a.extra->'random_proxy_empty_pool_policy' IS NOT DISTINCT FROM $3::jsonb->'random_proxy_empty_pool_policy'
 AND a.extra->'random_proxy_pool_scope' IS NOT DISTINCT FROM $3::jsonb->'random_proxy_pool_scope'
 AND a.extra->'random_proxy_pool_ids' IS NOT DISTINCT FROM $3::jsonb->'random_proxy_pool_ids'
 AND a.extra->'random_proxy_group_id' IS NOT DISTINCT FROM $3::jsonb->'random_proxy_group_id'
 AND a.extra->'random_proxy_region_fallback' IS NOT DISTINCT FROM $3::jsonb->'random_proxy_region_fallback'
 AND a.extra->'proxy_region_mode' IS NOT DISTINCT FROM $3::jsonb->'proxy_region_mode'
 AND a.extra->'proxy_region_country' IS NOT DISTINCT FROM $3::jsonb->'proxy_region_country'
 AND a.extra->'proxy_region_fallback_country' IS NOT DISTINCT FROM $3::jsonb->'proxy_region_fallback_country'
 AND (COALESCE(btrim(a.extra->>'proxy_region_mode'), 'off') <> 'billing' OR (
  a.credentials->>'billing_currency' IS NOT DISTINCT FROM $4::jsonb->>'billing_currency'
  AND a.credentials->>'price_country' IS NOT DISTINCT FROM $4::jsonb->>'price_country'))
 RETURNING a.id
)
INSERT INTO scheduler_outbox (event_type,account_id,payload)
SELECT $2,id,'{}'::jsonb FROM disabled`
