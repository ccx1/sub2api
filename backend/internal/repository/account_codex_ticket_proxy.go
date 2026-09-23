package repository

import (
	"context"
	"encoding/json"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func (r *accountRepository) GetCodexTicketProxy(ctx context.Context, id int64) (*service.Proxy, error) {
	proxy, err := r.client.Proxy.Get(ctx, id)
	if dbent.IsNotFound(err) {
		return nil, service.ErrProxyNotFound
	}
	if err != nil {
		return nil, err
	}
	return proxyEntityToService(proxy), nil
}

func (r *accountRepository) prepareCodexTicketProxyUpdate(ctx context.Context, ids []int64, updates map[string]any) (map[string]any, error) {
	mode, hasMode := updates[service.CodexTicketProxyModeExtraKey]
	_, hasID := updates[service.CodexTicketProxyIDExtraKey]
	_, hasStrategy := updates[service.CodexTicketProxyStrategyExtraKey]
	_, hasCredentialPolicy := updates[service.CodexTicketCredentialPolicyExtraKey]
	_, hasRegionMode := updates[service.ProxyRegionModeExtraKey]
	_, hasRegionCountry := updates[service.ProxyRegionCountryExtraKey]
	if !hasMode && !hasID && !hasStrategy && !hasCredentialPolicy && !hasRegionMode && !hasRegionCountry {
		return updates, nil
	}
	updates = copyJSONMap(updates)
	if hasMode && !hasID && mode != service.CodexTicketProxyModeFixed {
		updates[service.CodexTicketProxyIDExtraKey] = int64(0)
	}
	rows, err := clientFromContext(ctx, r.client).QueryContext(ctx,
		"SELECT extra FROM accounts WHERE id = ANY($1) AND deleted_at IS NULL ORDER BY id FOR NO KEY UPDATE", pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var current map[string]any
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &current); err != nil {
				return nil, err
			}
		}
		merged := service.MergeOpenAICodexTicketExtra(copyJSONMap(updates), current)
		merged = service.PreserveAccountProxyRegion(ctx, current, merged)
		if err := service.ValidateProxyRegionExtra(merged); err != nil {
			return nil, err
		}
		if err := service.ValidateCodexTicketProxyExtra(merged); err != nil {
			return nil, err
		}
	}
	return updates, rows.Err()
}

func needsCodexTicketProxyTransaction(ctx context.Context, extra map[string]any) bool {
	_, mode := extra[service.CodexTicketProxyModeExtraKey]
	_, id := extra[service.CodexTicketProxyIDExtraKey]
	_, strategy := extra[service.CodexTicketProxyStrategyExtraKey]
	_, credentialPolicy := extra[service.CodexTicketCredentialPolicyExtraKey]
	_, regionMode := extra[service.ProxyRegionModeExtraKey]
	_, regionCountry := extra[service.ProxyRegionCountryExtraKey]
	return (mode || id || strategy || credentialPolicy || regionMode || regionCountry) && dbent.TxFromContext(ctx) == nil
}

func (r *accountRepository) updateCodexTicketProxyExtra(ctx context.Context, id int64, updates map[string]any) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := r.UpdateExtra(dbent.NewTxContext(ctx, tx), id, updates); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func (r *accountRepository) bulkUpdateCodexTicketProxy(ctx context.Context, ids []int64, updates service.AccountBulkUpdate) (int64, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	count, err := r.BulkUpdate(dbent.NewTxContext(ctx, tx), ids, updates)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	r.syncSchedulerAccountSnapshots(ctx, ids)
	return count, nil
}

func preserveCodexTicketProxyExtraSQL(expression string) string {
	keys := "ARRAY['codex_ticket_proxy_mode','codex_ticket_proxy_id','codex_ticket_proxy_strategy','codex_ticket_credential_policy']::text[]"
	return "(" + expression + ") || COALESCE((SELECT jsonb_object_agg(key,value) FROM jsonb_each(COALESCE(extra,'{}'::jsonb)) WHERE key = ANY(" + keys + ") AND NOT ((" + expression + ") ? key)), '{}'::jsonb)"
}
