package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func accrueSharedPoolEarnings(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	billingAmount := cmd.BalanceCost
	if cmd.SharedPoolOwnerID > 0 && cmd.SharedPoolSettlementMultiplier != nil {
		var err error
		billingAmount, err = independentSharedBillingAmount(cmd)
		if err != nil {
			return err
		}
		if _, _, _, _, err := service.SplitSharedPoolSettlement(billingAmount, cmd.SharedPoolBaseCost, *cmd.SharedPoolSettlementMultiplier, 0, 0); err != nil {
			return err
		}
	}
	if cmd.SharedPoolOwnerID <= 0 || billingAmount <= 0 || cmd.SharedPoolOwnerID == cmd.UserID {
		return nil
	}
	if cmd.SharedPoolSettlementMultiplier == nil && (cmd.SubscriptionID != nil || cmd.SubscriptionCost > 0) {
		return nil
	}
	platformBPS, proxyBPS, err := sharedPoolBillingRates(ctx, tx, cmd)
	if err != nil {
		return err
	}
	if !cmd.UsesPlatformProxy {
		proxyBPS = 0
	}
	if cmd.SharedPoolSettlementMultiplier != nil {
		return accrueIndependentSharedEarnings(ctx, tx, cmd, result, platformBPS, proxyBPS)
	}
	platform, owner, err := service.SplitSharedPoolBillingAmount(cmd.BalanceCost, platformBPS, proxyBPS)
	if err != nil {
		return err
	}
	status := "available"
	if result.BalanceOverdrafted {
		status = "pending"
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO shared_pool_earnings (
			request_id, api_key_id, consumer_user_id, owner_user_id, account_id, group_id,
			billing_amount, platform_rate_bps, proxy_rate_bps, uses_platform_proxy,
			proxy_id, platform_amount, owner_amount, status
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
	`, cmd.RequestID, cmd.APIKeyID, cmd.UserID, cmd.SharedPoolOwnerID, cmd.AccountID, cmd.GroupID,
		cmd.BalanceCost, platformBPS, proxyBPS, cmd.UsesPlatformProxy, cmd.SharedPoolProxyID,
		platform.StringFixed(service.UsageBillingMonetaryScale), owner.StringFixed(service.UsageBillingMonetaryScale), status)
	return err
}

// 费率以成功计费事务中的配置为准，并随本次收益保存快照；后续改价不重算历史。
// 按请求快照核验共享分组；账号停用或分组改动不能阻断已提供服务的在途结算。
func sharedPoolBillingRates(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (int, int, error) {
	if (!cmd.SharedPoolGroup && cmd.SharedPoolSettlementMultiplier == nil) || cmd.GroupID <= 0 || (cmd.UsesPlatformProxy && (cmd.SharedPoolProxyID == nil || *cmd.SharedPoolProxyID <= 0)) {
		return 0, 0, service.ErrSharedPoolBillingInvalid
	}
	var platformBPS, proxyBPS int
	err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(ur.platform_rate_bps, s.platform_rate_bps),
		       COALESCE(ur.proxy_rate_bps, s.proxy_rate_bps)
		FROM shared_pool_accounts a
		JOIN shared_pool_settings s ON s.id = 1
		JOIN groups g ON g.id = $3
		LEFT JOIN shared_pool_user_rates ur ON ur.user_id = a.owner_user_id
		WHERE a.account_id = $1 AND a.owner_user_id = $2
	`, cmd.AccountID, cmd.SharedPoolOwnerID, cmd.GroupID).Scan(&platformBPS, &proxyBPS)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, service.ErrSharedPoolBillingInvalid
	}
	if err == nil && cmd.SharedPoolSettlementMultiplier != nil {
		if cmd.SharedPoolPlatformRateBPS == nil || cmd.SharedPoolProxyRateBPS == nil {
			return 0, 0, service.ErrSharedPoolBillingInvalid
		}
		platformBPS, proxyBPS = *cmd.SharedPoolPlatformRateBPS, *cmd.SharedPoolProxyRateBPS
	}
	return platformBPS, proxyBPS, err
}
