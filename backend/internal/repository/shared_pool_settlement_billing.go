package repository

import (
	"context"
	"database/sql"
	"math"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/shopspring/decimal"
)

// 订阅按本次实际消耗额度结算；余额与订阅来源必须互斥，避免重复计入下游费用。
func independentSharedBillingAmount(cmd *service.UsageBillingCommand) (float64, error) {
	if cmd.BalanceCost < 0 || cmd.SubscriptionCost < 0 || math.IsNaN(cmd.BalanceCost) || math.IsNaN(cmd.SubscriptionCost) || math.IsInf(cmd.BalanceCost, 0) || math.IsInf(cmd.SubscriptionCost, 0) {
		return 0, service.ErrSharedPoolBillingInvalid
	}
	if cmd.SubscriptionID != nil {
		if *cmd.SubscriptionID <= 0 || cmd.BalanceCost != 0 {
			return 0, service.ErrSharedPoolBillingInvalid
		}
		return cmd.SubscriptionCost, nil
	}
	if cmd.SubscriptionCost != 0 {
		return 0, service.ErrSharedPoolBillingInvalid
	}
	return cmd.BalanceCost, nil
}

func accrueIndependentSharedEarnings(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult, platformBPS, proxyBPS int) error {
	billingAmount, err := independentSharedBillingAmount(cmd)
	if err != nil {
		return err
	}
	amount, platform, owner, spread, err := service.SplitSharedPoolSettlement(billingAmount, cmd.SharedPoolBaseCost, *cmd.SharedPoolSettlementMultiplier, platformBPS, proxyBPS)
	if err != nil {
		return err
	}
	status := "available"
	if result.BalanceOverdrafted {
		status = "pending"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO shared_pool_earnings (
        request_id,api_key_id,consumer_user_id,owner_user_id,account_id,group_id,
        billing_amount,platform_rate_bps,proxy_rate_bps,uses_platform_proxy,proxy_id,
        platform_amount,owner_amount,status,base_amount,settlement_multiplier,settlement_amount,spread_amount)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::numeric,$16::numeric,$17::numeric,$18::numeric)`,
		cmd.RequestID, cmd.APIKeyID, cmd.UserID, cmd.SharedPoolOwnerID, cmd.AccountID, cmd.GroupID,
		billingAmount, platformBPS, proxyBPS, cmd.UsesPlatformProxy, cmd.SharedPoolProxyID,
		platform.StringFixed(8), owner.StringFixed(8), status,
		decimal.NewFromFloat(cmd.SharedPoolBaseCost).StringFixed(8), decimal.NewFromFloat(*cmd.SharedPoolSettlementMultiplier).String(), amount.StringFixed(8), spread.StringFixed(8))
	return err
}
