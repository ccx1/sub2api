package repository

import (
	"context"
	dbgroup "github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *accountRepository) SharedPoolDispatchGroup(ctx context.Context, id int64) (*service.Group, error) {
	row, err := r.client.Group.Query().Where(dbgroup.IDEQ(id), dbgroup.DeletedAtIsNil()).Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrGroupNotFound, nil)
	}
	return groupEntityToService(row), nil
}

func (r *accountRepository) SharedPoolSettlementTerms(ctx context.Context, ownerID int64) (*service.SharedPoolSettlementTerms, error) {
	return r.sharedPoolSettlementTerms(ctx, ownerID, "", "")
}

func (r *accountRepository) SharedPoolSettlementTermsForAccount(ctx context.Context, ownerID int64, platform, tier string) (*service.SharedPoolSettlementTerms, error) {
	return r.sharedPoolSettlementTerms(ctx, ownerID, platform, tier)
}

func (r *accountRepository) sharedPoolSettlementTerms(ctx context.Context, ownerID int64, platform, tier string) (*service.SharedPoolSettlementTerms, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT COALESCE(ur.settlement_multiplier,
		NULLIF(s.subscription_settlement_multipliers -> $2 ->> $3, '')::numeric,s.settlement_multiplier),
		COALESCE(ur.platform_rate_bps,s.platform_rate_bps),COALESCE(ur.proxy_rate_bps,s.proxy_rate_bps)
		FROM shared_pool_settings s JOIN users u ON u.id=$1 AND u.deleted_at IS NULL AND u.status='active'
		LEFT JOIN shared_pool_user_rates ur ON ur.user_id=u.id WHERE s.id=1`, ownerID, platform, tier)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrSharedPoolBillingInvalid
	}
	var terms service.SharedPoolSettlementTerms
	if err := rows.Scan(&terms.Multiplier, &terms.PlatformRateBPS, &terms.ProxyRateBPS); err != nil {
		return nil, err
	}
	if !terms.Valid() {
		return nil, service.ErrSharedPoolBillingInvalid
	}
	return &terms, rows.Err()
}
