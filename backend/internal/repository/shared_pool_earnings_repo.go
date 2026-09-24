package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"sort"
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

// 独立结算流水把消费价与供号结算价的差额保存在 platform_amount 里以保持账本守恒。
// 对外展示的平台收益只显示配置的平台/代理费；旧版流水没有 spread_amount，保持原口径。
const sharedPoolDisplayedPlatformAmountSQL = `platform_amount - COALESCE(spread_amount, 0)`

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
		platform_rate_bps, proxy_rate_bps, uses_platform_proxy, `+sharedPoolDisplayedPlatformAmountSQL+` AS platform_amount, owner_amount, effective_status, transfer_id, created_at,
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
		COALESCE(SUM(`+sharedPoolDisplayedPlatformAmountSQL+`), 0), COALESCE(SUM(billing_amount), 0)
		FROM `+sharedPoolEarningsReadSource+` WHERE ($1::bigint = 0 OR owner_user_id = $1)`, ownerID).
		Scan(&result.TotalEarned, &result.Available, &result.Pending, &result.Transferred, &result.PlatformAmount, &result.BillingAmount)
	return result, err
}

type sharedPoolUserEarningsAccount struct {
	UserID      int64
	Email       string
	Platform    string
	AccountType string
	Credentials []byte
	Extra       []byte
}

// UserEarnings 按供号用户聚合账本，并沿用流水和汇总接口的有效状态计算。
func (r *sharedPoolEarningsRepository) UserEarnings(ctx context.Context) ([]service.SharedPoolUserEarnings, error) {
	accounts, err := r.listSharedPoolUserAccounts(ctx)
	if err != nil {
		return nil, err
	}

	byUser := make(map[int64]*service.SharedPoolUserEarnings, len(accounts))
	mergeSharedPoolUserAccounts(byUser, accounts)
	if err := r.mergeSharedPoolUserEarnings(ctx, byUser); err != nil {
		return nil, err
	}
	return finalizeSharedPoolUserEarnings(byUser), nil
}

func mergeSharedPoolUserAccounts(byUser map[int64]*service.SharedPoolUserEarnings, accounts []sharedPoolUserEarningsAccount) {
	for _, account := range accounts {
		item := byUser[account.UserID]
		if item == nil {
			item = &service.SharedPoolUserEarnings{UserID: account.UserID, Email: account.Email, AccountTiers: []service.SharedPoolUserAccountTier{}}
			byUser[account.UserID] = item
		}
		if item.Email == "" {
			item.Email = account.Email
		}
		item.AccountCount++
		tier := sharedPoolAccountTier(account)
		found := false
		for i := range item.AccountTiers {
			if item.AccountTiers[i].Tier == tier {
				item.AccountTiers[i].Count++
				found = true
				break
			}
		}
		if !found {
			item.AccountTiers = append(item.AccountTiers, service.SharedPoolUserAccountTier{Tier: tier, Count: 1})
		}
	}
}

func (r *sharedPoolEarningsRepository) mergeSharedPoolUserEarnings(ctx context.Context, byUser map[int64]*service.SharedPoolUserEarnings) error {
	rows, err := r.db.QueryContext(ctx, `SELECT visible_earnings.owner_user_id, COALESCE(u.email, ''),
		COUNT(DISTINCT visible_earnings.account_id), COUNT(*), COALESCE(SUM(visible_earnings.billing_amount), 0),
		COALESCE(SUM(visible_earnings.owner_amount), 0), COALESCE(SUM(`+sharedPoolDisplayedPlatformAmountSQL+`), 0),
		COALESCE(SUM(visible_earnings.owner_amount) FILTER (WHERE visible_earnings.effective_status = 'available'), 0),
		COALESCE(SUM(visible_earnings.owner_amount) FILTER (WHERE visible_earnings.effective_status = 'pending'), 0),
		COALESCE(SUM(visible_earnings.owner_amount) FILTER (WHERE visible_earnings.effective_status = 'transferred'), 0)
		FROM `+sharedPoolEarningsReadSource+`
		LEFT JOIN users u ON u.id = visible_earnings.owner_user_id
		GROUP BY visible_earnings.owner_user_id, u.email
		ORDER BY COALESCE(SUM(visible_earnings.owner_amount), 0) DESC, visible_earnings.owner_user_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var item service.SharedPoolUserEarnings
		var earningsAccountCount int64
		if err := rows.Scan(&item.UserID, &item.Email, &earningsAccountCount, &item.EarningsCount,
			&item.BillingAmount, &item.TotalEarned, &item.PlatformAmount,
			&item.Available, &item.Pending, &item.Transferred); err != nil {
			return err
		}
		if existing := byUser[item.UserID]; existing != nil {
			if existing.Email == "" {
				existing.Email = item.Email
			}
			existing.EarningsCount = item.EarningsCount
			existing.BillingAmount = item.BillingAmount
			existing.TotalEarned = item.TotalEarned
			existing.PlatformAmount = item.PlatformAmount
			existing.Available = item.Available
			existing.Pending = item.Pending
			existing.Transferred = item.Transferred
			continue
		}
		// Keep historical earnings rows whose account was deleted from the
		// current shared-account registry visible to administrators.
		item.AccountCount = earningsAccountCount
		item.AccountTiers = []service.SharedPoolUserAccountTier{}
		byUser[item.UserID] = &item
	}
	return rows.Err()
}

func finalizeSharedPoolUserEarnings(byUser map[int64]*service.SharedPoolUserEarnings) []service.SharedPoolUserEarnings {
	result := make([]service.SharedPoolUserEarnings, 0, len(byUser))
	for _, item := range byUser {
		sort.Slice(item.AccountTiers, func(i, j int) bool { return item.AccountTiers[i].Tier < item.AccountTiers[j].Tier })
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].TotalEarned == result[j].TotalEarned {
			return result[i].UserID < result[j].UserID
		}
		return result[i].TotalEarned > result[j].TotalEarned
	})
	return result
}

func (r *sharedPoolEarningsRepository) listSharedPoolUserAccounts(ctx context.Context) ([]sharedPoolUserEarningsAccount, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT spa.owner_user_id, COALESCE(u.email, ''), a.platform, a.type, a.credentials, a.extra
		FROM shared_pool_accounts spa
		JOIN accounts a ON a.id = spa.account_id AND a.deleted_at IS NULL
		LEFT JOIN users u ON u.id = spa.owner_user_id
		ORDER BY spa.owner_user_id, spa.account_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	accounts := make([]sharedPoolUserEarningsAccount, 0)
	for rows.Next() {
		var account sharedPoolUserEarningsAccount
		if err := rows.Scan(&account.UserID, &account.Email, &account.Platform, &account.AccountType, &account.Credentials, &account.Extra); err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
	}
	return accounts, rows.Err()
}

func sharedPoolAccountTier(account sharedPoolUserEarningsAccount) string {
	var credentials, extra map[string]any
	if len(account.Credentials) > 0 {
		_ = json.Unmarshal(account.Credentials, &credentials)
	}
	if len(account.Extra) > 0 {
		_ = json.Unmarshal(account.Extra, &extra)
	}
	tier := service.SharedPoolOverviewTierForAccount(&service.Account{
		Platform: account.Platform, Type: account.AccountType, Credentials: credentials, Extra: extra,
	})
	if tier == "" {
		return "unknown"
	}
	return tier
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
