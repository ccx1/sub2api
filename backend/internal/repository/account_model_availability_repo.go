package repository

import (
	"context"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.ModelAvailabilityRepository = (*accountRepository)(nil)

// ListModelRateLimitedAccountIDsByReason 返回存在指定 reason 模型级限流条目的
// 可调度 API Key 账号 ID。仅扫描 status=active 且 schedulable=true 的账号：
// 被管理员暂停的账号不做后台探测，避免对暂停账号发起真实上游请求。
func (r *accountRepository) ListModelRateLimitedAccountIDsByReason(ctx context.Context, reason string, limit int) ([]int64, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 1000
	}
	rows, err := r.sql.QueryContext(ctx, `
		SELECT id
		FROM accounts
		WHERE deleted_at IS NULL
			AND type = $1
			AND status = $2
			AND schedulable IS TRUE
			AND jsonb_typeof(extra -> 'model_rate_limits') = 'object'
			AND EXISTS (
				SELECT 1
				FROM jsonb_each(extra -> 'model_rate_limits') AS limits(scope, value)
				WHERE jsonb_typeof(limits.value) = 'object'
					AND limits.value ->> 'reason' = $3
			)
		ORDER BY id ASC
		LIMIT $4`,
		service.AccountTypeAPIKey,
		service.StatusActive,
		reason,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

// ClearModelRateLimitIfReason 仅在该模型条目的 reason 仍然匹配时删除它，
// 避免误删期间被其它逻辑（如 404 冷却、429 限流）改写的新条目。
func (r *accountRepository) ClearModelRateLimitIfReason(ctx context.Context, id int64, scope, reason string) (bool, error) {
	scope = strings.TrimSpace(scope)
	reason = strings.TrimSpace(reason)
	if scope == "" || reason == "" {
		return false, nil
	}
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(
		ctx,
		`UPDATE accounts SET
			extra = jsonb_set(extra, '{model_rate_limits}'::text[], (extra -> 'model_rate_limits') - $1::text, true),
			updated_at = NOW()
		WHERE id = $2
			AND deleted_at IS NULL
			AND jsonb_typeof(extra -> 'model_rate_limits') = 'object'
			AND extra -> 'model_rate_limits' -> $1::text ->> 'reason' = $3`,
		scope,
		id,
		reason,
	)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue clear model rate limit by reason failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return true, nil
}

// RemoveModelMappingKeys 从 credentials.model_mapping 中原子删除指定的键。
// 只改 model_mapping 子树，不整列覆盖 credentials，避免与并发的凭据写入互相回滚。
// 删除后映射不得为空：空映射在语义上等于"允许所有模型"，与删除模型的意图相反。
func (r *accountRepository) RemoveModelMappingKeys(ctx context.Context, id int64, keys []string) (bool, error) {
	cleaned := make([]string, 0, len(keys))
	for _, key := range keys {
		if key = strings.TrimSpace(key); key != "" {
			cleaned = append(cleaned, key)
		}
	}
	if len(cleaned) == 0 {
		return false, nil
	}
	client := clientFromContext(ctx, r.client)
	result, err := client.ExecContext(
		ctx,
		`UPDATE accounts SET
			credentials = jsonb_set(credentials, '{model_mapping}'::text[], (credentials -> 'model_mapping') - $1::text[], true),
			updated_at = NOW()
		WHERE id = $2
			AND deleted_at IS NULL
			AND type = $3
			AND jsonb_typeof(credentials -> 'model_mapping') = 'object'
			AND (credentials -> 'model_mapping') ?| $1::text[]
			AND (credentials -> 'model_mapping') - $1::text[] <> '{}'::jsonb`,
		pq.Array(cleaned),
		id,
		service.AccountTypeAPIKey,
	)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, nil
	}
	if err := enqueueSchedulerOutbox(ctx, r.sql, service.SchedulerOutboxEventAccountChanged, &id, nil, nil); err != nil {
		logger.LegacyPrintf("repository.account", "[SchedulerOutbox] enqueue remove model mapping keys failed: account=%d err=%v", id, err)
	}
	r.syncSchedulerAccountSnapshot(ctx, id)
	return true, nil
}
