package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *sharedPoolRepository) SharedSettings(ctx context.Context) (*service.SharedPoolSettings, error) {
	var settings service.SharedPoolSettings
	var defaults, subscriptions, settlementMultipliers []byte
	err := r.db.QueryRowContext(ctx, `SELECT platform_rate_bps,proxy_rate_bps,max_concurrency,default_group_ids,subscription_group_ids,settlement_multiplier,subscription_settlement_multipliers,default_priority
        FROM shared_pool_settings WHERE id=1`).Scan(&settings.PlatformRateBPS, &settings.ProxyRateBPS,
		&settings.MaxConcurrency, &defaults, &subscriptions, &settings.SettlementMultiplier, &settlementMultipliers, &settings.DefaultPriority)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(defaults, &settings.DefaultGroupIDs); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(subscriptions, &settings.SubscriptionGroupIDs); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(settlementMultipliers, &settings.SubscriptionSettlementMultipliers); err != nil {
		return nil, err
	}
	if settings.DefaultGroupIDs == nil {
		settings.DefaultGroupIDs = service.SharedPoolDefaultGroupIDs{}
	}
	if settings.SubscriptionGroupIDs == nil {
		settings.SubscriptionGroupIDs = service.SharedPoolSubscriptionGroupIDs{}
	}
	if settings.SubscriptionSettlementMultipliers == nil {
		settings.SubscriptionSettlementMultipliers = map[string]map[string]float64{}
	}
	return &settings, nil
}

func (r *sharedPoolRepository) SaveSharedSettings(ctx context.Context, s *service.SharedPoolSettings) error {
	if s.DefaultPriority < 0 || s.DefaultPriority > 100 {
		return infraerrors.BadRequest("INVALID_SHARED_PRIORITY", "共享账号默认优先级须为0至100")
	}
	defaults, err := service.NormalizeSharedDefaultGroupIDs(s.DefaultGroupIDs)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var previous, previousMultipliers []byte
	if err = tx.QueryRowContext(ctx, `SELECT subscription_group_ids,subscription_settlement_multipliers FROM shared_pool_settings WHERE id=1 FOR UPDATE`).Scan(&previous, &previousMultipliers); err != nil {
		return err
	}
	subscriptions := s.SubscriptionGroupIDs
	// 旧客户端不传新字段时保留配置，显式空对象才清除所有档位路由。
	if subscriptions == nil {
		if err = json.Unmarshal(previous, &subscriptions); err != nil {
			return err
		}
	}
	if subscriptions == nil {
		subscriptions = service.SharedPoolSubscriptionGroupIDs{}
	}
	subscriptions, err = service.NormalizeSharedSubscriptionGroupIDs(subscriptions)
	if err != nil {
		return err
	}
	multipliers := s.SubscriptionSettlementMultipliers
	if multipliers == nil {
		if err = json.Unmarshal(previousMultipliers, &multipliers); err != nil {
			return err
		}
	}
	if multipliers == nil {
		multipliers = map[string]map[string]float64{}
	}
	normalizedMultipliers, err := service.NormalizeSharedSettlementMultipliers(multipliers)
	if err != nil {
		return err
	}
	multipliers = normalizedMultipliers
	settingsCopy := *s
	settingsCopy.SubscriptionSettlementMultipliers = multipliers
	if err = validateSharedSettingsGroups(ctx, tx, defaults, subscriptions); err != nil {
		return err
	}
	if err = validateSharedSettingsRates(ctx, tx, &settingsCopy); err != nil {
		return err
	}
	if err = saveSharedSettlementSettings(ctx, tx, &settingsCopy); err != nil {
		return err
	}
	defaultRaw, err := json.Marshal(defaults)
	if err != nil {
		return err
	}
	subscriptionRaw, err := json.Marshal(subscriptions)
	if err != nil {
		return err
	}
	multipliersRaw, err := json.Marshal(multipliers)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE shared_pool_settings SET platform_rate_bps=$1,proxy_rate_bps=$2,max_concurrency=$3,
        default_group_ids=$4,subscription_group_ids=$5,settlement_multiplier=$6,subscription_settlement_multipliers=$7,default_priority=$8,updated_at=NOW() WHERE id=1`,
		s.PlatformRateBPS, s.ProxyRateBPS, s.MaxConcurrency, string(defaultRaw), string(subscriptionRaw), settingsCopy.SettlementMultiplier, string(multipliersRaw), s.DefaultPriority)
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	s.DefaultGroupIDs, s.SubscriptionGroupIDs, s.SubscriptionSettlementMultipliers = defaults, subscriptions, multipliers
	return nil
}

func validateSharedSettingsGroups(ctx context.Context, tx *sql.Tx, defaults service.SharedPoolDefaultGroupIDs, subscriptions service.SharedPoolSubscriptionGroupIDs) error {
	for platform, ids := range defaults {
		for _, id := range ids {
			if err := validateSharedSettingsGroup(ctx, tx, platform, id, false); err != nil {
				return err
			}
		}
	}
	for platform, tiers := range subscriptions {
		if len(tiers) > 0 && len(defaults[platform]) == 0 {
			return infraerrors.BadRequest("SHARED_DEFAULT_REQUIRED", "配置订阅档位路由前，请先设置该平台的默认共享分组")
		}
		for _, ids := range tiers {
			for _, id := range ids {
				if err := validateSharedSettingsGroup(ctx, tx, platform, id, true); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func validateSharedSettingsGroup(ctx context.Context, tx *sql.Tx, platform string, id int64, allowExclusive bool) error {
	var valid bool
	err := tx.QueryRowContext(ctx, `SELECT status='active' AND platform=$2 AND rate_multiplier>0
        AND subscription_type='standard' AND ($3 OR NOT is_exclusive)
        FROM groups WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, id, platform, allowExclusive).Scan(&valid)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !valid) {
		return infraerrors.BadRequest("INVALID_SHARED_DEFAULT", "分组必须是已启用的同平台标准分组且收费倍率大于0；默认分组不可为专属分组")
	}
	return err
}

func validateSharedSettingsRates(ctx context.Context, tx *sql.Tx, s *service.SharedPoolSettings) error {
	var invalid bool
	err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM shared_pool_user_rates WHERE
        COALESCE(platform_rate_bps,$1)+COALESCE(proxy_rate_bps,$2)>10000)`, s.PlatformRateBPS, s.ProxyRateBPS).Scan(&invalid)
	if err != nil {
		return err
	}
	if invalid {
		return infraerrors.BadRequest("INVALID_SHARED_RATES", "新全局比例与部分用户覆盖比例合计超过100%")
	}
	return nil
}
