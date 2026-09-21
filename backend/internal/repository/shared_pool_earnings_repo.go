package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type sharedPoolEarningsRepository struct{ db *sql.DB }

// 读取不改账：欠费已补足的流水可领取，实际解冻留在转余额事务内执行。
const sharedPoolEarningsReadSource = `(SELECT e.*, CASE WHEN e.status = 'pending' AND EXISTS (
	SELECT 1 FROM users debtor WHERE debtor.id = e.consumer_user_id AND debtor.deleted_at IS NULL AND debtor.balance >= 0
) THEN 'available' ELSE e.status END AS effective_status FROM shared_pool_earnings e) visible_earnings`

func NewSharedPoolEarningsRepository(db *sql.DB) service.SharedPoolEarningsRepository {
	return &sharedPoolEarningsRepository{db: db}
}

func (r *sharedPoolEarningsRepository) List(ctx context.Context, ownerID int64, f service.SharedPoolEarningsFilter) (*service.SharedPoolEarningsPage, error) {
	if ownerID < 0 || f.AccountID < 0 || !validSharedPoolEarningsStatus(f.Status) {
		return nil, service.ErrSharedPoolBillingInvalid
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = 20
	}
	if f.PageSize > 100 {
		f.PageSize = 100
	}
	result := &service.SharedPoolEarningsPage{Items: []service.SharedPoolEarning{}, Page: f.Page, PageSize: f.PageSize}
	const where = ` WHERE ($1::bigint = 0 OR owner_user_id = $1) AND ($2::bigint = 0 OR account_id = $2) AND ($3::text = '' OR effective_status = $3)`
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+sharedPoolEarningsReadSource+where, ownerID, f.AccountID, f.Status).Scan(&result.Total)
	if err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, account_id, owner_user_id, group_id, billing_amount,
		platform_rate_bps, proxy_rate_bps, uses_platform_proxy, platform_amount, owner_amount, effective_status, transfer_id, created_at,
		base_amount,settlement_multiplier,settlement_amount,spread_amount
		FROM `+sharedPoolEarningsReadSource+where+` ORDER BY created_at DESC, id DESC LIMIT $4 OFFSET $5`,
		ownerID, f.AccountID, f.Status, f.PageSize, (int64(f.Page)-1)*int64(f.PageSize))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item service.SharedPoolEarning
		if err := rows.Scan(&item.ID, &item.AccountID, &item.OwnerUserID, &item.GroupID, &item.BillingAmount,
			&item.PlatformRateBPS, &item.ProxyRateBPS, &item.UsesPlatformProxy, &item.PlatformAmount,
			&item.OwnerAmount, &item.Status, &item.TransferID, &item.CreatedAt, &item.BaseAmount, &item.SettlementMultiplier, &item.SettlementAmount, &item.SpreadAmount); err != nil {
			return nil, err
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

func validSharedPoolEarningsStatus(status string) bool {
	return status == "" || status == "available" || status == "pending" || status == "transferred"
}

func (r *sharedPoolEarningsRepository) Summary(ctx context.Context, ownerID int64) (*service.SharedPoolEarningsSummary, error) {
	if ownerID < 0 {
		return nil, service.ErrSharedPoolBillingInvalid
	}
	result := &service.SharedPoolEarningsSummary{}
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(owner_amount), 0),
		COALESCE(SUM(owner_amount) FILTER (WHERE effective_status = 'available'), 0),
		COALESCE(SUM(owner_amount) FILTER (WHERE effective_status = 'pending'), 0),
		COALESCE(SUM(owner_amount) FILTER (WHERE effective_status = 'transferred'), 0),
		COALESCE(SUM(platform_amount), 0), COALESCE(SUM(billing_amount), 0)
		FROM `+sharedPoolEarningsReadSource+` WHERE ($1::bigint = 0 OR owner_user_id = $1)`, ownerID).
		Scan(&result.TotalEarned, &result.Available, &result.Pending, &result.Transferred, &result.PlatformAmount, &result.BillingAmount)
	return result, err
}

func (r *sharedPoolEarningsRepository) AccountTotals(ctx context.Context, ownerID int64, ids []int64) (map[int64]service.SharedPoolAccountEarnings, error) {
	if ownerID < 0 {
		return nil, service.ErrSharedPoolBillingInvalid
	}
	result := make(map[int64]service.SharedPoolAccountEarnings, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT account_id,
		COALESCE(SUM(owner_amount) FILTER (WHERE created_at >= $3), 0), COALESCE(SUM(owner_amount), 0)
		FROM shared_pool_earnings WHERE ($1::bigint = 0 OR owner_user_id = $1) AND account_id = ANY($2)
		GROUP BY account_id`, ownerID, pq.Array(ids), timezone.Today())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var item service.SharedPoolAccountEarnings
		if err := rows.Scan(&id, &item.TodayEarnings, &item.TotalEarnings); err != nil {
			return nil, err
		}
		result[id] = item
	}
	return result, rows.Err()
}

func (r *sharedPoolEarningsRepository) AccountWindow(ctx context.Context, ownerID, accountID int64, since time.Time) (*service.SharedPoolAccountWindowEarnings, error) {
	if ownerID < 0 || accountID <= 0 || since.IsZero() {
		return nil, service.ErrSharedPoolBillingInvalid
	}
	result := &service.SharedPoolAccountWindowEarnings{}
	err := r.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(billing_amount), 0), COALESCE(SUM(owner_amount), 0)
		FROM shared_pool_earnings WHERE ($1::bigint = 0 OR owner_user_id = $1) AND account_id = $2 AND created_at >= $3`,
		ownerID, accountID, since).Scan(&result.BillingAmount, &result.OwnerAmount)
	return result, err
}
