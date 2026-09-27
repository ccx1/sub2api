package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/proxy"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.ProxyQualityGuardRepository = (*proxyRepository)(nil)

// 运行时出口故障计数：由随机代理失败回调累加，巡检复检通过后清零。
func proxyQualityRuntimeFailureKey(proxyID int64) string {
	return "proxy:{pool}:quality_runtime_failures:" + strconv.FormatInt(proxyID, 10)
}

const (
	proxyQualityRuntimeFailureTTL   = 30 * time.Minute
	proxyQualityGuardInactiveStatus = "inactive"
)

// 只接管公共池代理：私有共享代理由其所属用户维护，已过期代理由到期服务处理。
func (r *proxyRepository) ListProxyQualityGuardCandidates(ctx context.Context) ([]service.ProxyQualityGuardCandidate, error) {
	items, err := r.client.Proxy.Query().WithGroup().Where(excludePrivateSharedProxies,
		proxy.Or(proxy.ExpiresAtIsNil(), proxy.ExpiresAtGT(time.Now()))).
		Order(dbent.Asc(proxy.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]service.ProxyQualityGuardCandidate, 0, len(items))
	for _, item := range items {
		out = append(out, service.ProxyQualityGuardCandidate{Proxy: *proxyEntityToService(item)})
	}
	if len(out) == 0 {
		return out, nil
	}
	fixed, err := r.fixedProxyAccountBindings(ctx)
	if err != nil {
		return nil, err
	}
	dynamic, err := r.dynamicProxyAccountCounts(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].FixedBoundCount = int64(len(fixed[out[i].Proxy.ID]))
		out[i].DynamicBoundCount = dynamic[out[i].Proxy.ID]
	}
	return out, nil
}

func (r *proxyRepository) ListProxyQualityGuardStates(ctx context.Context) (map[int64]*service.ProxyQualityGuardState, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT proxy_id, state, rounds, consecutive_failures, disabled_until,
	last_checked_at, last_success_at, last_failed_at, last_score, last_grade, last_error, updated_at
FROM proxy_quality_guard_states`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[int64]*service.ProxyQualityGuardState)
	for rows.Next() {
		state := &service.ProxyQualityGuardState{}
		var disabledUntil, checkedAt, successAt, failedAt sql.NullTime
		var score sql.NullInt64
		if err := rows.Scan(&state.ProxyID, &state.State, &state.Rounds, &state.ConsecutiveFailures, &disabledUntil,
			&checkedAt, &successAt, &failedAt, &score, &state.LastGrade, &state.LastError, &state.UpdatedAt); err != nil {
			return nil, err
		}
		state.DisabledUntil = nullTimePtr(disabledUntil)
		state.LastCheckedAt = nullTimePtr(checkedAt)
		state.LastSuccessAt = nullTimePtr(successAt)
		state.LastFailedAt = nullTimePtr(failedAt)
		if score.Valid {
			value := int(score.Int64)
			state.LastScore = &value
		}
		out[state.ProxyID] = state
	}
	return out, rows.Err()
}

func nullTimePtr(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	t := value.Time
	return &t
}

const upsertProxyQualityGuardStateSQL = `
INSERT INTO proxy_quality_guard_states (proxy_id, state, rounds, consecutive_failures, disabled_until,
	last_checked_at, last_success_at, last_failed_at, last_score, last_grade, last_error, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW())
ON CONFLICT (proxy_id) DO UPDATE SET state = EXCLUDED.state, rounds = EXCLUDED.rounds,
	consecutive_failures = EXCLUDED.consecutive_failures, disabled_until = EXCLUDED.disabled_until,
	last_checked_at = EXCLUDED.last_checked_at, last_success_at = EXCLUDED.last_success_at,
	last_failed_at = EXCLUDED.last_failed_at, last_score = EXCLUDED.last_score,
	last_grade = EXCLUDED.last_grade, last_error = EXCLUDED.last_error, updated_at = NOW()`

func saveProxyQualityGuardState(ctx context.Context, exec sqlExecutor, state *service.ProxyQualityGuardState) error {
	var score any
	if state.LastScore != nil {
		score = *state.LastScore
	}
	_, err := exec.ExecContext(ctx, upsertProxyQualityGuardStateSQL, state.ProxyID, state.State, state.Rounds,
		state.ConsecutiveFailures, state.DisabledUntil, state.LastCheckedAt, state.LastSuccessAt, state.LastFailedAt,
		score, state.LastGrade, truncateProxyQualityText(state.LastError, 1000))
	return err
}

func appendProxyQualityGuardEvent(ctx context.Context, exec sqlExecutor, event service.ProxyQualityGuardEvent) error {
	_, err := exec.ExecContext(ctx, `INSERT INTO proxy_quality_guard_events (proxy_id, proxy_name, action, round, reason)
VALUES ($1, $2, $3, $4, $5)`, event.ProxyID, truncateProxyQualityText(event.ProxyName, 100), event.Action, event.Round,
		truncateProxyQualityText(event.Reason, 1000))
	return err
}

func truncateProxyQualityText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

func (r *proxyRepository) SaveProxyQualityGuardState(ctx context.Context, state *service.ProxyQualityGuardState) error {
	return saveProxyQualityGuardState(ctx, r.sql, state)
}

func (r *proxyRepository) AppendProxyQualityGuardEvent(ctx context.Context, event service.ProxyQualityGuardEvent) error {
	return appendProxyQualityGuardEvent(ctx, r.sql, event)
}

func (r *proxyRepository) ListProxyQualityGuardEvents(ctx context.Context, limit int) ([]service.ProxyQualityGuardEvent, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := r.sql.QueryContext(ctx, `SELECT e.id, e.proxy_id,
	COALESCE(NULLIF(e.proxy_name, ''), p.name, ''), e.action, e.round, e.reason, e.created_at
FROM proxy_quality_guard_events e
LEFT JOIN proxies p ON p.id = e.proxy_id
ORDER BY e.created_at DESC, e.id DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]service.ProxyQualityGuardEvent, 0)
	for rows.Next() {
		var event service.ProxyQualityGuardEvent
		if err := rows.Scan(&event.ID, &event.ProxyID, &event.ProxyName, &event.Action, &event.Round, &event.Reason, &event.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

// setProxyStatusForQualityGuard 沿用代理更新路径切换状态，确保探测快照失效和调度外发与手动编辑一致。
// expected 为空表示不校验当前状态；状态已被管理员改动时返回 false 且不写入。
func (r *proxyRepository) setProxyStatusForQualityGuard(ctx context.Context, proxyID int64, expected, status string,
	state *service.ProxyQualityGuardState, event service.ProxyQualityGuardEvent) (bool, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	current, err := lockProxyProbeIdentity(txCtx, tx.Client(), proxyID)
	if err != nil {
		return false, err
	}
	changed := false
	if current.status != status && (expected == "" || current.status == expected) {
		entity, err := tx.Client().Proxy.Get(txCtx, proxyID)
		if err != nil {
			return false, err
		}
		proxyIn := proxyEntityToService(entity)
		proxyIn.Status = status
		if _, err := updateProxyAndInvalidateProbeSnapshots(txCtx, tx.Client(), proxyIn); err != nil {
			return false, err
		}
		changed = true
	}
	exec := tx.Client()
	if err := saveProxyQualityGuardState(txCtx, exec, state); err != nil {
		return false, err
	}
	if changed || event.Action != service.ProxyQualityGuardActionRestored {
		if err := appendProxyQualityGuardEvent(txCtx, exec, event); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return changed, nil
}

func (r *proxyRepository) DisableProxyForQualityGuard(ctx context.Context, state *service.ProxyQualityGuardState, event service.ProxyQualityGuardEvent) (bool, error) {
	// 状态改为 inactive 后随机池不再返回该代理，账号下次选路时由分配器自动换出口。
	return r.setProxyStatusForQualityGuard(ctx, state.ProxyID, service.StatusActive, proxyQualityGuardInactiveStatus, state, event)
}

// 只恢复由巡检禁用的代理；管理员期间手动改动过的状态不覆盖。
func (r *proxyRepository) RestoreProxyForQualityGuard(ctx context.Context, state *service.ProxyQualityGuardState, event service.ProxyQualityGuardEvent) (bool, error) {
	return r.setProxyStatusForQualityGuard(ctx, state.ProxyID, proxyQualityGuardInactiveStatus, service.StatusActive, state, event)
}

func (r *proxyRepository) DeleteProxyForQualityGuard(ctx context.Context, state *service.ProxyQualityGuardState, event service.ProxyQualityGuardEvent) (bool, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	if _, err := lockProxyProbeIdentity(txCtx, tx.Client(), state.ProxyID); err != nil {
		return false, err
	}
	// 在代理行锁内复核固定绑定，避免与账号保存并发时删掉刚被绑定的代理。
	var fixed int64
	if err := scanSingleInt64(txCtx, tx.Client(), `SELECT COUNT(*) FROM accounts
WHERE deleted_at IS NULL AND (
 (proxy_id = $1 AND COALESCE(lower(btrim(extra->>'proxy_mode')), '') <> 'random')
 OR (lower(btrim(extra->>'codex_ticket_proxy_mode')) = 'fixed' AND extra->>'codex_ticket_proxy_id' = $1::text))`,
		&fixed, state.ProxyID); err != nil {
		return false, err
	}
	if fixed > 0 {
		return false, service.ErrProxyInUse
	}
	affected, err := tx.Client().Proxy.Delete().Where(proxy.IDEQ(state.ProxyID)).Exec(txCtx)
	if err != nil {
		return false, err
	}
	if err := saveProxyQualityGuardState(txCtx, tx.Client(), state); err != nil {
		return false, err
	}
	if err := appendProxyQualityGuardEvent(txCtx, tx.Client(), event); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	_ = r.ClearProxyQualityGuardRuntimeFailures(ctx, state.ProxyID)
	return affected > 0, nil
}

func (r *proxyRepository) ResetProxyQualityGuardState(ctx context.Context, proxyID int64, event service.ProxyQualityGuardEvent) error {
	states, err := r.ListProxyQualityGuardStates(ctx)
	if err != nil {
		return err
	}
	current := states[proxyID]
	name := ""
	if proxyEntity, err := r.GetByID(ctx, proxyID); err == nil {
		name = proxyEntity.Name
	} else if !errors.Is(err, service.ErrProxyNotFound) {
		return err
	} else if current == nil {
		return service.ErrProxyNotFound
	}
	event.ProxyName = name
	next := &service.ProxyQualityGuardState{ProxyID: proxyID, State: service.ProxyQualityGuardStateActive}
	if current != nil {
		next.LastCheckedAt, next.LastSuccessAt, next.LastFailedAt = current.LastCheckedAt, current.LastSuccessAt, current.LastFailedAt
		next.LastScore, next.LastGrade = current.LastScore, current.LastGrade
	}
	if current != nil && current.State == service.ProxyQualityGuardStateDisabled {
		_, err := r.RestoreProxyForQualityGuard(ctx, next, event)
		return err
	}
	if err := saveProxyQualityGuardState(ctx, r.sql, next); err != nil {
		return err
	}
	return appendProxyQualityGuardEvent(ctx, r.sql, event)
}

func (r *proxyRepository) CountProxyQualityGuardRuntimeFailures(ctx context.Context, proxyIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(proxyIDs))
	if r.proxyPoolRedis == nil || len(proxyIDs) == 0 {
		return out, nil
	}
	keys := make([]string, len(proxyIDs))
	for i, id := range proxyIDs {
		keys[i] = proxyQualityRuntimeFailureKey(id)
	}
	values, err := r.proxyPoolRedis.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	for i, value := range values {
		text, ok := value.(string)
		if !ok {
			continue
		}
		if count, err := strconv.ParseInt(text, 10, 64); err == nil {
			out[proxyIDs[i]] = count
		}
	}
	return out, nil
}

func (r *proxyRepository) ClearProxyQualityGuardRuntimeFailures(ctx context.Context, proxyID int64) error {
	if r.proxyPoolRedis == nil {
		return nil
	}
	return r.proxyPoolRedis.Del(ctx, proxyQualityRuntimeFailureKey(proxyID)).Err()
}

func scanSingleInt64(ctx context.Context, exec sqlExecutor, query string, dest *int64, args ...any) error {
	rows, err := exec.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return fmt.Errorf("query returned no rows")
	}
	if err := rows.Scan(dest); err != nil {
		return err
	}
	return rows.Err()
}
