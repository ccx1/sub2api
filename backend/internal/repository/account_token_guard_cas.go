package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const tokenGuardStateCASSQL = `WITH eligible AS (
 SELECT id FROM accounts a WHERE a.id=$1 AND a.deleted_at IS NULL
 AND a.platform='openai' AND a.type='oauth' AND a.parent_account_id IS NULL
 AND a.status IN ('active','error') AND a.updated_at=$15 AND a.credentials=$16::jsonb
 AND a.proxy_id IS NOT DISTINCT FROM $17 AND a.status=$18 AND a.schedulable=$19
 AND (a.auto_pause_on_expired IS NOT TRUE OR a.expires_at IS NULL OR a.expires_at>NOW())
 FOR UPDATE)
 INSERT INTO account_token_guard_states(
 account_id,account_name,account_status,schedulable,probe_state,probe_detail,latency_ms,fail_streak,
 last_probe_at,last_fix_at,last_fix_action,last_fix_result,updated_at)
 SELECT $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,clock_timestamp() FROM eligible
 WHERE $14::timestamptz IS NULL OR EXISTS (SELECT 1 FROM account_token_guard_states WHERE account_id=$1)
 ON CONFLICT(account_id) DO UPDATE SET
 account_name=EXCLUDED.account_name,account_status=EXCLUDED.account_status,schedulable=EXCLUDED.schedulable,
 probe_state=EXCLUDED.probe_state,probe_detail=EXCLUDED.probe_detail,latency_ms=EXCLUDED.latency_ms,
 fail_streak=EXCLUDED.fail_streak,last_probe_at=EXCLUDED.last_probe_at,
 last_fix_at=COALESCE(EXCLUDED.last_fix_at,account_token_guard_states.last_fix_at),
 last_fix_action=CASE WHEN EXCLUDED.last_fix_at IS NULL THEN account_token_guard_states.last_fix_action ELSE EXCLUDED.last_fix_action END,
 last_fix_result=CASE WHEN EXCLUDED.last_fix_at IS NULL THEN account_token_guard_states.last_fix_result ELSE EXCLUDED.last_fix_result END,
 updated_at=GREATEST(clock_timestamp(),account_token_guard_states.updated_at+INTERVAL '1 microsecond')
 WHERE account_token_guard_states.updated_at=$14 AND $13::boolean
 RETURNING updated_at`

// 同一语句锁定账号快照并比较守护状态版本，防止跨实例旧结果回写。
func (r *accountTokenGuardRepository) UpsertStateIfUnchanged(ctx context.Context, state service.AccountTokenGuardState) (time.Time, error) {
	account := state.AccountVersion
	if account == nil || account.ID != state.AccountID {
		return time.Time{}, service.ErrAccountTokenGuardStale
	}
	credentials, err := json.Marshal(normalizeJSONMap(account.Credentials))
	if err != nil {
		return time.Time{}, err
	}
	var expected any
	if !state.UpdatedAt.IsZero() {
		expected = state.UpdatedAt
	}
	rows, err := r.db.QueryContext(ctx, tokenGuardStateCASSQL, state.AccountID, state.AccountName, state.AccountStatus,
		state.Schedulable, state.ProbeState, state.ProbeDetail, state.LatencyMS, state.FailStreak, state.LastProbeAt,
		state.LastFixAt, state.LastFixAction, state.LastFixResult, !state.UpdatedAt.IsZero(), expected,
		account.UpdatedAt, string(credentials), account.ProxyID, account.Status, account.Schedulable)
	if err != nil {
		return time.Time{}, err
	}
	return readTokenGuardVersion(rows)
}

// RETURNING 后仍须读取终止结果，提交/出队错误不能被首行成功掩盖。
func readTokenGuardVersion(rows *sql.Rows) (time.Time, error) {
	var version time.Time
	if !rows.Next() {
		err := rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return version, err
		}
		if closeErr != nil {
			return version, closeErr
		}
		return version, service.ErrAccountTokenGuardStale
	}
	err := rows.Scan(&version)
	for rows.Next() {
		if err == nil {
			err = errors.New("multiple token guard versions returned")
		}
	}
	if err == nil {
		err = rows.Err()
	}
	closeErr := rows.Close()
	if err != nil {
		return time.Time{}, err
	}
	return version, closeErr
}
