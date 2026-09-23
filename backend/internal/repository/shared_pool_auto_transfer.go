package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

type sharedPoolAutoTransferRepository struct {
	db *sql.DB
}

func NewSharedPoolAutoTransferRepository(db *sql.DB) service.SharedPoolAutoTransferRepository {
	return &sharedPoolAutoTransferRepository{db: db}
}

func (r *sharedPoolAutoTransferRepository) Get(ctx context.Context, userID int64) (*service.SharedPoolAutoTransferSettings, error) {
	if userID <= 0 {
		return nil, service.ErrSharedPoolAutoTransferInvalid
	}
	result, err := scanSharedPoolAutoTransfer(r.db.QueryRowContext(ctx, `
		SELECT enabled, threshold, daily_time, COALESCE(last_run_date::text, '')
		FROM shared_pool_auto_transfer_settings WHERE user_id = $1`, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return &service.SharedPoolAutoTransferSettings{Threshold: 1, DailyTime: "00:00", Timezone: timezone.Name()}, nil
	}
	return result, err
}

func (r *sharedPoolAutoTransferRepository) Save(ctx context.Context, userID int64, input service.SharedPoolAutoTransferUpdate) (*service.SharedPoolAutoTransferSettings, error) {
	if userID <= 0 || service.ValidateSharedPoolAutoTransfer(input) != nil {
		return nil, service.ErrSharedPoolAutoTransferInvalid
	}
	// 更新设置不重置执行日期，防止同一天修改时间或门槛后再次自动入账。
	result, err := scanSharedPoolAutoTransfer(r.db.QueryRowContext(ctx, `
		INSERT INTO shared_pool_auto_transfer_settings (user_id, enabled, threshold, daily_time)
		SELECT id, $2, $3::numeric, $4 FROM users WHERE id = $1 AND deleted_at IS NULL
		ON CONFLICT (user_id) DO UPDATE SET enabled = EXCLUDED.enabled,
			threshold = EXCLUDED.threshold, daily_time = EXCLUDED.daily_time, updated_at = NOW()
		RETURNING enabled, threshold, daily_time, COALESCE(last_run_date::text, '')`,
		userID, input.Enabled, decimal.NewFromFloat(input.Threshold).String(), input.DailyTime))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUserNotFound
	}
	return result, err
}

func scanSharedPoolAutoTransfer(row *sql.Row) (*service.SharedPoolAutoTransferSettings, error) {
	result := &service.SharedPoolAutoTransferSettings{Timezone: timezone.Name()}
	if err := row.Scan(&result.Enabled, &result.Threshold, &result.DailyTime, &result.LastRunDate); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *sharedPoolAutoTransferRepository) DueUserIDs(ctx context.Context, now time.Time, afterID int64, limit int) ([]int64, error) {
	if limit <= 0 {
		return []int64{}, nil
	}
	localNow := now.In(timezone.Location())
	rows, err := r.db.QueryContext(ctx, `SELECT s.user_id FROM shared_pool_auto_transfer_settings s
		JOIN users u ON u.id = s.user_id AND u.deleted_at IS NULL AND u.status = 'active'
		WHERE s.enabled AND s.user_id > $1 AND (s.last_run_date IS NULL OR s.last_run_date < $2::date)
			AND s.daily_time <= $3 ORDER BY s.user_id LIMIT $4`,
		afterID, localNow.Format("2006-01-02"), localNow.Format("15:04"), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return nil, err
		}
		ids = append(ids, userID)
	}
	return ids, rows.Err()
}
