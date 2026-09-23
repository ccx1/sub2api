package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func isSharedPoolUniqueConflict(err error) bool {
	e, ok := err.(*pq.Error)
	return ok && e.Code == "23505"
}
func serviceSharedConflict() error {
	return infraerrors.Conflict("SHARED_ACCOUNT_EXISTS", "该上游账号已经登记，请勿重复共享")
}

func (r *sharedPoolRepository) SetSharedAccountState(ctx context.Context, id int64, state service.SharedPoolAccountState) error {
	if err := validateSharedStateActor(state); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = lockSharedAccount(ctx, tx, state.OwnerID, id); err != nil {
		return err
	}
	var adminDisabled, assigned bool
	var rawExtra []byte
	if err = tx.QueryRowContext(ctx, `SELECT s.admin_disabled,s.assigned,a.extra FROM shared_pool_accounts s JOIN accounts a ON a.id=s.account_id WHERE s.account_id=$1`, id).Scan(&adminDisabled, &assigned, &rawExtra); err != nil {
		return err
	}
	extra := map[string]any{}
	if len(rawExtra) > 0 {
		if err = json.Unmarshal(rawExtra, &extra); err != nil {
			return err
		}
	}
	consented, patch, err := prepareSharedDispatchState(extra, &state)
	if err != nil {
		return err
	}
	patch, err = prepareSharedTierState(ctx, tx, id, state.SubscriptionTier, patch)
	if err != nil {
		return err
	}
	enabled, disabled, groups := state.Enabled, state.AdminDisabled, state.GroupIDs
	effectiveDisabled := adminDisabled
	if disabled != nil {
		effectiveDisabled = *disabled
	}
	if enabled != nil && *enabled && effectiveDisabled {
		return infraerrors.Forbidden("SHARED_ADMIN_DISABLED", "账号已被管理员停用")
	}
	if state.OwnerID > 0 && assigned {
		groups = nil
	}
	oldGroups, err := sharedAccountGroupIDs(ctx, tx, id)
	if err != nil {
		return err
	}
	if groups == nil && len(state.DefaultGroupIDs) > 0 && !assigned && len(oldGroups) == 0 && consented && enabled != nil && *enabled {
		ids := append([]int64(nil), state.DefaultGroupIDs...)
		groups = &ids
	}
	if state.Priority != nil {
		if _, err = tx.ExecContext(ctx, `UPDATE accounts SET priority=$2 WHERE id=$1`, id, *state.Priority); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `UPDATE account_groups SET priority=$2 WHERE account_id=$1`, id, *state.Priority); err != nil {
			return err
		}
	}
	if groups != nil {
		manualAssignment := state.OwnerID == 0 && state.GroupIDs != nil
		if err = assignSharedGroups(ctx, tx, id, *groups, consented, manualAssignment); err != nil {
			return err
		}
	}
	if enabled != nil && *enabled && !consented {
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_groups ag JOIN groups g ON g.id=ag.group_id
            WHERE ag.account_id=$1 AND g.is_shared_pool AND g.status='active' AND g.deleted_at IS NULL`, id).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return infraerrors.BadRequest("SHARED_GROUP_REQUIRED", "请先配置可用的共享分组")
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE shared_pool_accounts SET enabled=COALESCE($2,enabled),
        admin_disabled=COALESCE($3,admin_disabled),assigned=assigned OR $4,updated_at=NOW() WHERE account_id=$1`, id, enabled, disabled, groups != nil)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts a SET extra=(COALESCE(a.extra,'{}'::jsonb) || jsonb_build_object(
        'shared_pool_owner_id',s.owner_user_id,'shared_pool_enabled',s.enabled,'shared_pool_admin_disabled',s.admin_disabled)||$2::jsonb)
        - CASE WHEN $2::jsonb->'shared_pool_subscription_tier'='null'::jsonb THEN 'shared_pool_subscription_tier' ELSE '' END,updated_at=NOW()
        FROM shared_pool_accounts s WHERE a.id=$1 AND s.account_id=a.id`, id, patch)
	if err != nil {
		return err
	}
	if groups != nil {
		oldGroups = mergeGroupIDs(oldGroups, *groups)
	}
	if err = enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &id, nil, buildSchedulerGroupPayload(oldGroups)); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	r.accounts.syncSchedulerAccountSnapshot(ctx, id)
	return nil
}

func sharedAccountGroupIDs(ctx context.Context, tx *sql.Tx, id int64) ([]int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT group_id FROM account_groups WHERE account_id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var groupID int64
		if err = rows.Scan(&groupID); err != nil {
			return nil, err
		}
		ids = append(ids, groupID)
	}
	return ids, rows.Err()
}

func assignSharedGroups(ctx context.Context, tx *sql.Tx, id int64, ids []int64, consented, manual bool) error {
	if len(ids) > 50 {
		return infraerrors.BadRequest("TOO_MANY_GROUPS", "最多关联50个分组")
	}
	var platform, kind string
	if err := tx.QueryRowContext(ctx, `SELECT platform,type FROM accounts WHERE id=$1`, id).Scan(&platform, &kind); err != nil {
		return err
	}
	for _, groupID := range ids {
		var valid bool
		err := tx.QueryRowContext(ctx, `SELECT (is_shared_pool OR $4) AND status='active' AND platform=$2 AND platform<>'composite'
            AND (CASE WHEN $4 AND $5 THEN subscription_type IN ('standard','subscription')
                ELSE subscription_type='standard' AND NOT is_exclusive END)
            AND (NOT require_oauth_only OR $3='oauth')
            FROM groups WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, groupID, platform, kind, consented, manual).Scan(&valid)
		if err != nil || !valid {
			return infraerrors.BadRequest("INVALID_SHARED_GROUP", "分组不存在、与账号不兼容或账号尚未授权参与普通分组调度")
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM account_groups WHERE account_id=$1 AND NOT(group_id=ANY(COALESCE($2::bigint[],'{}'::bigint[])))`, id, pq.Array(ids)); err != nil {
		return err
	}
	for _, groupID := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO account_groups(account_id,group_id,priority)
        SELECT $1,$2,priority FROM accounts WHERE id=$1 ON CONFLICT DO NOTHING`, id, groupID); err != nil {
			return err
		}
	}
	return nil
}

func (r *sharedPoolRepository) SharedUserRates(ctx context.Context) ([]service.SharedPoolUserRate, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT r.user_id,u.email,r.platform_rate_bps,r.proxy_rate_bps,r.settlement_multiplier
        FROM shared_pool_user_rates r JOIN users u ON u.id=r.user_id ORDER BY r.user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]service.SharedPoolUserRate, 0)
	for rows.Next() {
		var item service.SharedPoolUserRate
		if err = rows.Scan(&item.UserID, &item.Email, &item.PlatformRateBPS, &item.ProxyRateBPS, &item.SettlementMultiplier); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *sharedPoolRepository) SaveSharedUserRate(ctx context.Context, rate service.SharedPoolUserRate) error {
	if rate.SettlementMultiplier != nil && !service.ValidSharedPoolSettlementMultiplier(*rate.SettlementMultiplier) {
		return infraerrors.BadRequest("INVALID_SHARED_SETTLEMENT", "结算倍率必须为 0 到 100 的有限数值")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var valid bool
	err = tx.QueryRowContext(ctx, `SELECT COALESCE($1,platform_rate_bps)+COALESCE($2,proxy_rate_bps)<=10000
        FROM shared_pool_settings WHERE id=1 FOR UPDATE`, rate.PlatformRateBPS, rate.ProxyRateBPS).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return infraerrors.BadRequest("INVALID_SHARED_RATES", "分成比例合计不得超过100%")
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO shared_pool_user_rates(user_id,platform_rate_bps,proxy_rate_bps,settlement_multiplier)
		VALUES($1,$2,$3,$4) ON CONFLICT(user_id) DO UPDATE SET platform_rate_bps=$2,proxy_rate_bps=$3,settlement_multiplier=$4,updated_at=NOW()`, rate.UserID, rate.PlatformRateBPS, rate.ProxyRateBPS, rate.SettlementMultiplier)
	if err != nil {
		return err
	}
	return tx.Commit()
}
