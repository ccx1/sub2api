package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

type sharedPoolAutoTransferRun struct {
	userID int64
	today  string
	clock  string
}

func (r *sharedPoolAutoTransferRepository) RunDue(ctx context.Context, userID int64, now time.Time) (*service.SharedPoolEarningsTransfer, error) {
	if userID <= 0 {
		return nil, service.ErrSharedPoolAutoTransferInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	localNow := now.In(timezone.Location())
	run := sharedPoolAutoTransferRun{userID: userID, today: localNow.Format("2006-01-02"), clock: localNow.Format("15:04")}
	result, err := run.execute(ctx, tx)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

func (run sharedPoolAutoTransferRun) execute(ctx context.Context, tx *sql.Tx) (*service.SharedPoolEarningsTransfer, error) {
	// 和手动转入一样先锁用户，门槛检查到转账完成之间不允许其它转入消耗收益。
	if err := lockSharedPoolAutoTransferUser(ctx, tx, run.userID); err != nil {
		return nil, err
	}
	threshold, due, err := run.lockSettings(ctx, tx)
	if err != nil || !due {
		return nil, err
	}
	if err := releaseRepaidSharedPoolEarnings(ctx, tx, run.userID); err != nil {
		return nil, err
	}
	available, err := run.available(ctx, tx)
	if err != nil {
		return nil, err
	}
	var result *service.SharedPoolEarningsTransfer
	if available.GreaterThanOrEqual(threshold) {
		result, err = transferSharedPoolEarnings(ctx, tx, run.userID)
		if err != nil {
			return nil, err
		}
	}
	// 未到门槛也算当天已检查；失败则让整个事务回滚，下一轮调度重试。
	_, err = tx.ExecContext(ctx, `UPDATE shared_pool_auto_transfer_settings
		SET last_run_date = $2::date, updated_at = NOW() WHERE user_id = $1`, run.userID, run.today)
	return result, err
}

func lockSharedPoolAutoTransferUser(ctx context.Context, tx *sql.Tx, userID int64) error {
	var status string
	err := tx.QueryRowContext(ctx, `SELECT status FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrUserNotFound
	}
	if err != nil {
		return err
	}
	if status != service.StatusActive {
		return service.ErrUserNotActive
	}
	return nil
}

func (run sharedPoolAutoTransferRun) lockSettings(ctx context.Context, tx *sql.Tx) (decimal.Decimal, bool, error) {
	var enabled bool
	var rawThreshold, dailyTime, lastRunDate string
	err := tx.QueryRowContext(ctx, `SELECT enabled, threshold::text, daily_time, COALESCE(last_run_date::text, '')
		FROM shared_pool_auto_transfer_settings WHERE user_id = $1 FOR UPDATE`, run.userID).
		Scan(&enabled, &rawThreshold, &dailyTime, &lastRunDate)
	if errors.Is(err, sql.ErrNoRows) {
		return decimal.Zero, false, nil
	}
	if err != nil {
		return decimal.Zero, false, err
	}
	if !enabled || lastRunDate >= run.today || dailyTime > run.clock {
		return decimal.Zero, false, nil
	}
	threshold, err := decimal.NewFromString(rawThreshold)
	if err == nil && !threshold.IsPositive() {
		err = fmt.Errorf("invalid shared pool auto transfer threshold: %s", rawThreshold)
	}
	return threshold, err == nil, err
}

func (run sharedPoolAutoTransferRun) available(ctx context.Context, tx *sql.Tx) (decimal.Decimal, error) {
	var rawAmount string
	err := tx.QueryRowContext(ctx, `SELECT COALESCE(SUM(owner_amount), 0)::text FROM shared_pool_earnings
		WHERE owner_user_id = $1 AND status = 'available' AND owner_amount > 0`, run.userID).Scan(&rawAmount)
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromString(rawAmount)
}
