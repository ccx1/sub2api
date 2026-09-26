package repository

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

// 资料、私有代理与重授权凭证同成同败；不覆盖并行变化的共享开关和保护状态。
func (r *sharedPoolRepository) UpdateSharedAccount(ctx context.Context, ownerID, id int64, in service.SharedPoolAccountUpdate) error {
	dailyCooldown, err := sharedDailyCooldownJSON(in.DailyCooldown)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockSharedAccount(ctx, tx, ownerID, id); err != nil {
		return err
	}
	if in.Credentials != nil {
		_, err = tx.ExecContext(ctx, `UPDATE shared_pool_accounts SET credential_fingerprint=$2,updated_at=NOW() WHERE account_id=$1`, id, in.Fingerprint)
		if isSharedPoolUniqueConflict(err) {
			return serviceSharedConflict()
		}
		if err != nil {
			return err
		}
	}
	var credentials any
	if in.Credentials != nil {
		raw, e := json.Marshal(in.Credentials)
		if e != nil {
			return e
		}
		credentials = string(raw)
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET name=$2,concurrency=$3,
		credentials=CASE WHEN $4::jsonb IS NULL THEN credentials ELSE $4::jsonb||jsonb_build_object('_token_version',
			GREATEST(COALESCE((credentials->>'_token_version')::bigint,0)+1,(EXTRACT(EPOCH FROM clock_timestamp())*1000)::bigint)) END,
		proxy_id=CASE WHEN $5 THEN $6::bigint ELSE proxy_id END,
		extra=CASE WHEN NOT $5 THEN COALESCE(extra,'{}'::jsonb)
			WHEN $6::bigint IS NULL THEN COALESCE(extra,'{}'::jsonb)||'{"proxy_mode":"random","random_proxy_empty_pool_policy":"reject"}'::jsonb
			ELSE COALESCE(extra,'{}'::jsonb)-'proxy_mode'-'random_proxy_empty_pool_policy' END,
		updated_at=NOW() WHERE id=$1`, id, in.Name, in.Concurrency, credentials, in.ProxyChanged, in.ProxyID)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET extra=jsonb_set(extra,'{anti_degrade,max_concurrency}',to_jsonb($2::integer),true)
		WHERE id=$1 AND (extra->'anti_degradation'='true'::jsonb OR extra#>'{anti_degrade,enabled}'='true'::jsonb)
		AND jsonb_typeof(extra->'anti_degrade')='object'`, id, in.Concurrency)
	if err != nil {
		return err
	}
	if dailyCooldown != nil {
		_, err = tx.ExecContext(ctx, `UPDATE accounts SET extra=COALESCE(extra,'{}'::jsonb)||jsonb_build_object($2::text,$3::jsonb)
			WHERE id=$1`, id, service.DailyCooldownExtraKey, string(dailyCooldown))
		if err != nil {
			return err
		}
	}
	if in.ExcelBPSChanged {
		bpsExtra := in.ExcelBPSExtra
		if bpsExtra == nil {
			bpsExtra = map[string]any{}
		}
		raw, e := json.Marshal(bpsExtra)
		if e != nil {
			return e
		}
		// 整族替换，避免关闭或改范围后残留旧模型列表与子选项。
		if _, err = tx.ExecContext(ctx, `UPDATE accounts SET extra=(COALESCE(extra,'{}'::jsonb)-$2::text[])||$3::jsonb WHERE id=$1`,
			id, pq.Array(service.ExcelBPSExtraKeys()), string(raw)); err != nil {
			return err
		}
	}
	if in.Credentials != nil {
		// 原凭证的隐私/额度快照不能用于新授权。错误状态由明确的连接测试恢复。
		_, err = tx.ExecContext(ctx, `UPDATE accounts SET extra=extra-'privacy_mode'-'codex_usage_updated_at'-'codex_5h_used_percent'-'codex_7d_used_percent' WHERE id=$1`, id)
		if err != nil {
			return err
		}
	}
	if err = enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	r.accounts.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func sharedDailyCooldownJSON(cooldown *service.SharedPoolDailyCooldown) ([]byte, error) {
	if cooldown == nil {
		return nil, nil
	}
	value := cooldown.ExtraValue()
	if err := service.ValidateDailyCooldownExtra(map[string]any{service.DailyCooldownExtraKey: value}); err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
