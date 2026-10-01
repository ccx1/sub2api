package repository

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.AccountTokenGuardAccountStore = (*accountRepository)(nil)

const tokenGuardCandidatesSQL = `SELECT a.id,a.name,a.platform,a.type,
 COALESCE(a.credentials,'{}'::jsonb),COALESCE(a.extra,'{}'::jsonb),a.proxy_id,
 a.status,COALESCE(a.error_message,''),a.schedulable,a.updated_at,a.expires_at,
 a.auto_pause_on_expired,a.parent_account_id,a.proxy_fallback_origin_id
 FROM accounts a WHERE a.deleted_at IS NULL AND a.platform='openai' AND a.type='oauth'
 AND a.parent_account_id IS NULL AND a.status IN ('active','error')
 AND (a.auto_pause_on_expired IS NOT TRUE OR a.expires_at IS NULL OR a.expires_at>NOW())
 AND (cardinality($1::bigint[])=0 OR EXISTS (
 SELECT 1 FROM account_groups ag JOIN groups g ON g.id=ag.group_id
 WHERE ag.account_id=a.id AND ag.group_id=ANY($1::bigint[]) AND g.deleted_at IS NULL))
 ORDER BY a.id`

func (r *accountRepository) ListTokenGuardCandidates(ctx context.Context, groupIDs []int64) ([]service.Account, error) {
	for _, id := range groupIDs {
		if id <= 0 {
			return nil, errors.New("invalid token guard group ID")
		}
	}
	if groupIDs == nil {
		groupIDs = []int64{}
	}
	rows, err := r.sql.QueryContext(ctx, tokenGuardCandidatesSQL, pq.Array(groupIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	accounts := make([]service.Account, 0)
	for rows.Next() {
		var account service.Account
		var credentials, extra []byte
		if err := rows.Scan(&account.ID, &account.Name, &account.Platform, &account.Type,
			&credentials, &extra, &account.ProxyID, &account.Status, &account.ErrorMessage,
			&account.Schedulable, &account.UpdatedAt, &account.ExpiresAt, &account.AutoPauseOnExpired,
			&account.ParentAccountID, &account.ProxyFallbackOriginID); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(credentials, &account.Credentials); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(extra, &account.Extra); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

const tokenGuardRepairSQL = `WITH updated AS (
 UPDATE accounts a SET credentials=COALESCE($1::jsonb,a.credentials),status='active',error_message='',
 updated_at=GREATEST(clock_timestamp(),a.updated_at+INTERVAL '1 microsecond')
 WHERE a.id=$2 AND a.deleted_at IS NULL AND a.platform='openai' AND a.type='oauth'
 AND a.parent_account_id IS NULL AND a.status IN ('active','error')
 AND a.updated_at=$3 AND a.credentials=$4::jsonb AND a.status=$5 AND a.schedulable=$6
 AND a.proxy_id IS NOT DISTINCT FROM $7
 AND (a.auto_pause_on_expired IS NOT TRUE OR a.expires_at IS NULL OR a.expires_at>NOW())
 RETURNING a.id,a.updated_at), queued AS (
 INSERT INTO scheduler_outbox(event_type,account_id,group_id,payload)
 SELECT $8,updated.id,NULL,NULL FROM updated RETURNING account_id)
 SELECT updated.updated_at FROM updated JOIN queued ON queued.account_id=updated.id`

func (r *accountRepository) ApplyTokenGuardRepair(ctx context.Context, expected *service.Account, credentials map[string]any) (time.Time, error) {
	if expected == nil {
		return time.Time{}, service.ErrAccountTokenGuardStale
	}
	oldJSON, err := json.Marshal(normalizeJSONMap(expected.Credentials))
	if err != nil {
		return time.Time{}, err
	}
	var replacement any
	if credentials != nil {
		encoded, err := json.Marshal(normalizeJSONMap(credentials))
		if err != nil {
			return time.Time{}, err
		}
		replacement = string(encoded)
	}
	rows, err := r.sql.QueryContext(ctx, tokenGuardRepairSQL, replacement, expected.ID, expected.UpdatedAt,
		string(oldJSON), expected.Status, expected.Schedulable, expected.ProxyID, service.SchedulerOutboxEventAccountChanged)
	if err != nil {
		return time.Time{}, err
	}
	version, err := readTokenGuardVersion(rows)
	if err != nil {
		return time.Time{}, err
	}
	r.syncSchedulerAccountSnapshotDetached(ctx, expected.ID)
	return version, nil
}
