package service

import (
	"github.com/shopspring/decimal"
	"math"
)

// 独立结算只对基础用量费用乘供号倍率；消费端的分组、用户覆盖和高峰倍率不参与分成。
func SplitSharedPoolSettlement(paid, base, multiplier float64, platformBPS, proxyBPS int) (settlement, platform, owner, spread decimal.Decimal, err error) {
	if !ValidSharedPoolSettlementMultiplier(multiplier) || base < 0 || paid < 0 || math.IsNaN(base) || math.IsNaN(paid) || math.IsInf(base, 0) || math.IsInf(paid, 0) {
		err = ErrSharedPoolBillingInvalid
		return
	}
	settlement = decimal.NewFromFloat(base).Mul(decimal.NewFromFloat(multiplier)).Round(UsageBillingMonetaryScale)
	collected := decimal.NewFromFloat(paid).Round(UsageBillingMonetaryScale)
	if settlement.GreaterThan(collected) {
		err = ErrSharedPoolBillingInvalid
		return
	}
	fee, net, splitErr := SplitSharedPoolBillingAmount(settlement.InexactFloat64(), platformBPS, proxyBPS)
	if splitErr != nil {
		err = splitErr
		return
	}
	owner, spread = net, collected.Sub(settlement)
	platform = spread.Add(fee)
	return
}
