package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type spendGuardLockedKey struct {
	key    string
	name   string
	status string
}

func (r *apiKeyRepository) beginSpendGuardTx(ctx context.Context) (*sql.Tx, error) {
	db, ok := r.sql.(interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	})
	if !ok {
		return nil, errors.New("spend guard transaction database unavailable")
	}
	return db.BeginTx(ctx, nil)
}

func lockSpendGuardKey(ctx context.Context, tx *sql.Tx, id int64) (spendGuardLockedKey, error) {
	var key spendGuardLockedKey
	err := tx.QueryRowContext(ctx, `SELECT key, name, status FROM api_keys
		WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&key.key, &key.name, &key.status)
	if errors.Is(err, sql.ErrNoRows) {
		return key, service.ErrAPIKeyNotFound
	}
	return key, err
}

// 先锁 API Key 再读冻结记录，所有状态转换遵守同一锁顺序。
func readSpendGuardFreeze(ctx context.Context, tx *sql.Tx, id int64) (bool, *time.Time, error) {
	var released sql.NullTime
	err := tx.QueryRowContext(ctx, `SELECT released_at FROM api_key_spend_guard_freezes WHERE api_key_id = $1`, id).Scan(&released)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, err
	}
	if released.Valid {
		return false, &released.Time, nil
	}
	return true, nil, nil
}

func (r *apiKeyRepository) FreezeAPIKeyForSpendGuard(ctx context.Context, in service.SpendGuardFreezeInput) (string, bool, error) {
	tx, err := r.beginSpendGuardTx(ctx)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = tx.Rollback() }()
	key, err := lockSpendGuardKey(ctx, tx, in.APIKeyID)
	if err != nil {
		return "", false, err
	}
	frozen, released, err := readSpendGuardFreeze(ctx, tx, in.APIKeyID)
	if err != nil {
		return "", false, err
	}
	// 管理员在本轮统计后解冻时，旧统计不得将其立即重新冻结。
	if frozen || key.status != service.StatusAPIKeyActive || !sameSpendGuardRelease(released, in.ReleasedAt) {
		return key.key, false, nil
	}
	if err := saveSpendGuardFreeze(ctx, tx, in); err != nil {
		return "", false, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE api_keys SET status = 'disabled', updated_at = NOW() WHERE id = $1`, in.APIKeyID); err != nil {
		return "", false, err
	}
	if err := insertSpendGuardEvent(ctx, tx, service.SpendGuardEvent{APIKeyID: in.APIKeyID, Name: key.name, Action: "frozen", Reason: in.Reason}); err != nil {
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return key.key, true, nil
}

func sameSpendGuardRelease(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(*b)
}

func saveSpendGuardFreeze(ctx context.Context, tx *sql.Tx, in service.SpendGuardFreezeInput) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO api_key_spend_guard_freezes(api_key_id, frozen_at, reason)
		VALUES ($1, clock_timestamp(), $2) ON CONFLICT (api_key_id) DO UPDATE
		SET frozen_at = EXCLUDED.frozen_at, reason = EXCLUDED.reason, released_at = NULL`, in.APIKeyID, in.Reason)
	return err
}

func (r *apiKeyRepository) UnfreezeAPIKeyForSpendGuard(ctx context.Context, id int64) (string, bool, error) {
	tx, err := r.beginSpendGuardTx(ctx)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = tx.Rollback() }()
	key, err := lockSpendGuardKey(ctx, tx, id)
	if err != nil {
		return "", false, err
	}
	frozen, _, err := readSpendGuardFreeze(ctx, tx, id)
	if err != nil || !frozen {
		return key.key, false, err
	}
	// 先解除冻结再启用，数据库触发器才允许本事务恢复状态。
	if _, err := tx.ExecContext(ctx, `UPDATE api_key_spend_guard_freezes SET released_at = clock_timestamp() WHERE api_key_id = $1`, id); err != nil {
		return "", false, err
	}
	if key.status == service.StatusAPIKeyDisabled {
		if _, err := tx.ExecContext(ctx, `UPDATE api_keys SET status = 'active', updated_at = NOW() WHERE id = $1`, id); err != nil {
			return "", false, err
		}
	}
	if err := insertSpendGuardEvent(ctx, tx, service.SpendGuardEvent{APIKeyID: id, Name: key.name, Action: "unfrozen", Reason: "manual"}); err != nil {
		return "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", false, err
	}
	return key.key, true, nil
}

func insertSpendGuardEvent(ctx context.Context, tx *sql.Tx, event service.SpendGuardEvent) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO spend_guard_events(api_key_id, api_key_name, action, reason)
		VALUES ($1, $2, $3, $4)`, event.APIKeyID, event.Name, event.Action, event.Reason)
	return err
}
