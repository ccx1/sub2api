package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

func (r *sharedPoolEarningsRepository) Transfer(ctx context.Context, userID int64) (*service.SharedPoolEarningsTransfer, error) {
	if userID <= 0 {
		return nil, service.ErrSharedPoolBillingInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	result, err := transferSharedPoolEarnings(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func transferSharedPoolEarnings(ctx context.Context, tx *sql.Tx, userID int64) (*service.SharedPoolEarningsTransfer, error) {
	result, rawAmount, err := claimSharedPoolEarnings(ctx, tx, userID)
	if err != nil {
		return nil, err
	}
	var rawBalance string
	// 金额和余额快照始终以十进制字符串进入 SQL，避免转 float64 后损失八位精度。
	// 共享收益转入不算充值，也不触发邀请返利。
	err = tx.QueryRowContext(ctx, `UPDATE users SET balance = balance + $1::numeric, updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL RETURNING balance::text`, rawAmount, userID).Scan(&rawBalance)
	if err != nil {
		return nil, err
	}
	balance, err := decimal.NewFromString(rawBalance)
	if err != nil {
		return nil, err
	}
	result.Balance, _ = balance.Float64()
	_, err = tx.ExecContext(ctx, `UPDATE shared_pool_earnings_transfers SET amount = $1::numeric, balance_after = $2::numeric WHERE id = $3`, rawAmount, rawBalance, result.ID)
	if err != nil {
		return nil, err
	}
	if err := insertSharedPoolTransferRedeem(ctx, tx, result.ID); err != nil {
		return nil, err
	}
	return result, nil
}

func insertSharedPoolTransferRedeem(ctx context.Context, tx *sql.Tx, transferID int64) error {
	// 从转账快照读取金额、归属和时间；专用类型不计充值，已用状态禁止再次兑换。
	result, err := tx.ExecContext(ctx, `INSERT INTO redeem_codes (code, type, value, status, used_by, used_at, created_at, notes)
		SELECT 'SHARED-' || id::text, 'shared_pool_transfer', amount, 'used',
			user_id, created_at, created_at, '共享收益转入余额'
		FROM shared_pool_earnings_transfers WHERE id = $1 AND amount > 0`, transferID)
	if err != nil {
		return fmt.Errorf("record shared pool transfer history: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("verify shared pool transfer history: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("record shared pool transfer history: expected one source, got %d", rows)
	}
	return nil
}

func claimSharedPoolEarnings(ctx context.Context, tx *sql.Tx, userID int64) (*service.SharedPoolEarningsTransfer, string, error) {
	result := &service.SharedPoolEarningsTransfer{}
	var rawBalance, ownerStatus string
	// 先锁用户再锁收益，和扣费事务锁顺序一致；同一用户的并发转入串行执行。
	err := tx.QueryRowContext(ctx, `SELECT balance::text, status FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&rawBalance, &ownerStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", service.ErrUserNotFound
	}
	if err != nil {
		return nil, "", err
	}
	if ownerStatus != service.StatusActive {
		return nil, "", service.ErrUserNotActive
	}
	if err := releaseRepaidSharedPoolEarnings(ctx, tx, userID); err != nil {
		return nil, "", err
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO shared_pool_earnings_transfers (user_id, amount, balance_after)
		VALUES ($1, 0, $2::numeric) RETURNING id`, userID, rawBalance).Scan(&result.ID)
	if err != nil {
		return nil, "", err
	}
	var rawAmount string
	err = tx.QueryRowContext(ctx, `WITH claimed AS (
		SELECT id FROM shared_pool_earnings WHERE owner_user_id = $1 AND status = 'available' AND owner_amount > 0 ORDER BY id FOR UPDATE
	), moved AS (
		UPDATE shared_pool_earnings e SET status = 'transferred', transfer_id = $2, transferred_at = NOW()
		FROM claimed c WHERE e.id = c.id RETURNING e.owner_amount
	) SELECT COALESCE(SUM(owner_amount), 0)::text FROM moved`, userID, result.ID).Scan(&rawAmount)
	if err != nil {
		return nil, "", err
	}
	amount, err := decimal.NewFromString(rawAmount)
	if err != nil {
		return nil, "", err
	}
	if !amount.IsPositive() {
		return nil, "", service.ErrSharedPoolEarningsEmpty
	}
	result.Amount, _ = amount.Float64()
	return result, rawAmount, nil
}

// 当前非负余额证明原透支已补足；若调用者再次欠费，保守延后到再次补足时领取。
func releaseRepaidSharedPoolEarnings(ctx context.Context, tx *sql.Tx, ownerID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE shared_pool_earnings e
		SET status = 'available', released_at = NOW(), release_reason = 'consumer_balance_restored'
		FROM users debtor WHERE e.owner_user_id = $1 AND e.status = 'pending'
		AND debtor.id = e.consumer_user_id AND debtor.deleted_at IS NULL AND debtor.balance >= 0`, ownerID)
	return err
}
