package repository

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.AccountExcelBPSGroupRepository = (*accountRepository)(nil)

func (r *accountRepository) MoveExcelBPSOn403(ctx context.Context, account *service.Account) (bool, error) {
	target, enabled := account.ExcelBPS403GroupTarget()
	if !enabled {
		return false, nil
	}
	if dbent.TxFromContext(ctx) != nil {
		return r.moveExcelBPSOn403InTx(ctx, account, target)
	}
	tx, err := r.client.Tx(ctx)
	if errors.Is(err, dbent.ErrTxStarted) {
		return r.moveExcelBPSOn403InTx(ctx, account, target)
	}
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	changed, err := r.moveExcelBPSOn403InTx(dbent.NewTxContext(ctx, tx), account, target)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if changed {
		r.syncSchedulerAccountSnapshot(ctx, account.ID)
	}
	return changed, nil
}

func (r *accountRepository) moveExcelBPSOn403InTx(ctx context.Context, account *service.Account, target int64) (bool, error) {
	client := clientFromContext(ctx, r.client)
	// 与共享开关、人工分组绑定统一为账号行先于分组行加锁。
	locked, err := r.lockExcelBPS403Account(ctx, account, target)
	if err != nil || locked == nil {
		return false, err
	}
	if err := r.validateExcelBPS403Target(ctx, locked, target); err != nil {
		return false, err
	}
	rows, err := client.QueryContext(ctx, `SELECT group_id FROM account_groups WHERE account_id = $1 ORDER BY group_id FOR UPDATE`, account.ID)
	if err != nil {
		return false, err
	}
	var current []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return false, err
		}
		current = append(current, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return false, err
	}
	expected := slices.Clone(account.GroupIDs)
	slices.Sort(expected)
	// A concurrent manual edit or a preceding 403 has already changed routing.
	if !slices.Equal(current, expected) || (target == 0 && len(current) == 0) || (len(current) == 1 && current[0] == target) {
		return false, nil
	}
	if _, shared := locked.Extra[service.SharedPoolOwnerKey]; shared {
		if _, err := client.ExecContext(ctx, "UPDATE shared_pool_accounts SET assigned=TRUE,updated_at=NOW() WHERE account_id=$1", account.ID); err != nil {
			return false, err
		}
	}
	if _, err := client.ExecContext(ctx, `DELETE FROM account_groups WHERE account_id = $1 AND ($2 = 0 OR group_id <> $2)`, account.ID, target); err != nil {
		return false, err
	}
	// Keep an existing destination membership's priority and model restrictions.
	if target > 0 && !slices.Contains(current, target) {
		if _, err := client.ExecContext(ctx, `INSERT INTO account_groups (account_id, group_id, priority) VALUES ($1, $2, 1)`, account.ID, target); err != nil {
			return false, err
		}
	}
	if _, err := client.ExecContext(ctx, `UPDATE accounts SET updated_at = NOW() WHERE id = $1`, account.ID); err != nil {
		return false, err
	}
	payload := buildSchedulerGroupPayload(mergeGroupIDs(current, []int64{target}))
	if err := enqueueSchedulerOutbox(ctx, client, service.SchedulerOutboxEventAccountGroupsChanged, &account.ID, nil, payload); err != nil {
		return false, err
	}
	return true, nil
}

func (r *accountRepository) lockExcelBPS403Account(ctx context.Context, account *service.Account, target int64) (*service.Account, error) {
	credentials, err := json.Marshal(account.Credentials)
	if err != nil {
		return nil, err
	}
	client := clientFromContext(ctx, r.client)
	rows, err := client.QueryContext(ctx, `SELECT extra FROM accounts
WHERE id = $1 AND deleted_at IS NULL AND parent_account_id IS NULL
  AND platform = 'openai' AND type = 'oauth' AND credentials = $2::jsonb
  AND extra -> 'openai_excel_bps' = 'true'::jsonb
  AND extra -> 'openai_excel_bps_auto_move_on_403' = 'true'::jsonb
  AND extra -> 'openai_excel_bps_403_target_group_id' = $3::jsonb
FOR UPDATE`, account.ID, string(credentials), strconv.FormatInt(target, 10))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	var raw []byte
	if err := rows.Scan(&raw); err != nil {
		return nil, err
	}
	locked := &service.Account{ID: account.ID, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth}
	if err := json.Unmarshal(raw, &locked.Extra); err != nil {
		return nil, err
	}
	return locked, rows.Err()
}

func (r *accountRepository) validateExcelBPS403Target(ctx context.Context, account *service.Account, target int64) error {
	if target == 0 {
		return nil
	}
	client := clientFromContext(ctx, r.client)
	rows, err := client.QueryContext(ctx, `SELECT platform, status, subscription_type, is_exclusive, is_shared_pool, require_oauth_only FROM groups
WHERE id = $1 AND deleted_at IS NULL AND platform IN ('openai', 'composite') FOR SHARE`, target)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return service.ErrGroupNotFound
	}
	group := &service.Group{ID: target}
	if err := rows.Scan(&group.Platform, &group.Status, &group.SubscriptionType, &group.IsExclusive, &group.IsSharedPool, &group.RequireOAuthOnly); err != nil {
		return err
	}
	if _, shared := account.Extra[service.SharedPoolOwnerKey]; shared &&
		!sharedAccountGroupAllowed(group, account.Platform, account.Type, service.SharedPoolDispatchConsented(account)) {
		return infraerrors.BadRequest("INVALID_SHARED_GROUP", "分组与账号不兼容，或账号尚未授权参与普通分组调度")
	}
	return rows.Err()
}
