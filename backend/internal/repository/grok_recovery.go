package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const clearGrokTempUnschedCAS = `
WITH updated AS (
 UPDATE accounts SET temp_unschedulable_until = NULL,
 temp_unschedulable_reason = NULL, updated_at = NOW()
 WHERE id = $1 AND deleted_at IS NULL AND platform = 'grok'
 AND status = 'active' AND schedulable IS TRUE
 AND temp_unschedulable_until = $2
 AND temp_unschedulable_reason = $3
 RETURNING id
)
INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
SELECT $4, id, NULL, NULL FROM updated`

// 清理和 outbox 写入同一语句，旧刷新不能覆盖后来的保护状态。
func (r *accountRepository) ClearTempUnschedulableIfUnchanged(ctx context.Context, id int64, expectedUntil time.Time, expectedReason string) (bool, error) {
	if id <= 0 || expectedUntil.IsZero() || expectedReason == "" {
		return false, nil
	}
	result, err := r.sql.ExecContext(ctx, clearGrokTempUnschedCAS,
		id, expectedUntil, expectedReason, service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return false, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}

const clearGrokModelRateLimitCAS = `
WITH updated AS (
 UPDATE accounts SET extra = jsonb_set(COALESCE(extra, '{}'::jsonb),
 '{model_rate_limits}', COALESCE(extra->'model_rate_limits', '{}'::jsonb) - $2, true),
 updated_at = NOW()
 WHERE id = $1 AND deleted_at IS NULL AND platform = 'grok'
 AND status = 'active' AND schedulable IS TRUE
 AND extra #> ARRAY['model_rate_limits', $2]::text[] = $3::jsonb
 RETURNING id
)
INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
SELECT $4, id, NULL, NULL FROM updated`

func (r *accountRepository) ClearGrokModelRateLimitIfUnchanged(ctx context.Context, id int64, model string, expected map[string]any) (bool, error) {
	if id <= 0 || model == "" || len(expected) == 0 {
		return false, nil
	}
	raw, err := json.Marshal(expected)
	if err != nil {
		return false, err
	}
	result, err := r.sql.ExecContext(ctx, clearGrokModelRateLimitCAS,
		id, model, string(raw), service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return false, err
	}
	changed, err := result.RowsAffected()
	if err != nil || changed == 0 {
		return false, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, id)
	return true, nil
}
