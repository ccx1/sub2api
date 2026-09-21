package repository

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func (r *groupRepository) sharedPoolSettlementByAccounts(ctx context.Context, accounts []*service.Account) (map[int64]*service.SharedPoolSettlementTerms, error) {
	termsByAccount := make(map[int64]*service.SharedPoolSettlementTerms)
	byID, ids := sharedPoolSettlementAccounts(accounts)
	if len(ids) == 0 {
		return termsByAccount, nil
	}
	rows, err := r.sql.QueryContext(ctx, `SELECT spa.account_id,spa.owner_user_id,s.settlement_multiplier,
		s.subscription_settlement_multipliers,ur.settlement_multiplier,
		COALESCE(ur.platform_rate_bps,s.platform_rate_bps),COALESCE(ur.proxy_rate_bps,s.proxy_rate_bps)
		FROM shared_pool_accounts spa JOIN users u ON u.id=spa.owner_user_id AND u.deleted_at IS NULL AND u.status='active'
		CROSS JOIN shared_pool_settings s LEFT JOIN shared_pool_user_rates ur ON ur.user_id=u.id
		WHERE spa.account_id=ANY($1) AND s.id=1`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, ownerID int64
		var settings service.SharedPoolSettings
		var tiers []byte
		var multiplier sql.NullFloat64
		var terms service.SharedPoolSettlementTerms
		if err := rows.Scan(&id, &ownerID, &settings.SettlementMultiplier, &tiers, &multiplier, &terms.PlatformRateBPS, &terms.ProxyRateBPS); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(tiers, &settings.SubscriptionSettlementMultipliers); err != nil {
			return nil, err
		}
		acc := byID[id]
		if acc == nil {
			continue
		}
		rate := service.SharedPoolUserRate{UserID: ownerID}
		if multiplier.Valid {
			rate.SettlementMultiplier = &multiplier.Float64
		}
		terms.Multiplier = service.EffectiveSharedSettlementMultiplierForTier(&settings, []service.SharedPoolUserRate{rate}, ownerID, acc.Platform, service.SharedPoolOverviewTierForAccount(acc))
		termsByAccount[id] = &terms
	}
	return termsByAccount, rows.Err()
}

func sharedPoolSettlementAccounts(accounts []*service.Account) (map[int64]*service.Account, []int64) {
	byID, ids := make(map[int64]*service.Account), make([]int64, 0, len(accounts))
	for _, acc := range accounts {
		if acc == nil || byID[acc.ID] != nil || !service.SharedPoolDispatchConsented(acc) {
			continue
		}
		byID[acc.ID], ids = acc, append(ids, acc.ID)
	}
	return byID, ids
}
