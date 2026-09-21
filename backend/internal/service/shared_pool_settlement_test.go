package service

import (
	"github.com/stretchr/testify/require"
	"math"
	"testing"
)

func TestSharedSettlementIndependentOfConsumerRate(t *testing.T) {
	for _, paid := range []float64{5, 10, 15} {
		amount, platform, owner, spread, err := SplitSharedPoolSettlement(paid, 10, 0.5, 500, 100)
		require.NoError(t, err)
		require.Equal(t, "5.00000000", amount.StringFixed(8))
		require.Equal(t, "4.70000000", owner.StringFixed(8))
		require.Equal(t, QuantizeUsageBillingAmount(paid), platform.Add(owner).InexactFloat64())
		require.Equal(t, paid-5, spread.InexactFloat64())
	}
}

func TestSharedSettlementRejectsUnfundedAndInvalid(t *testing.T) {
	for _, rate := range []float64{-1, 101, math.Inf(1), math.NaN()} {
		_, _, _, _, err := SplitSharedPoolSettlement(10, 10, rate, 500, 100)
		require.Error(t, err)
	}
	_, _, _, _, err := SplitSharedPoolSettlement(4, 10, .5, 500, 100)
	require.ErrorIs(t, err, ErrSharedPoolBillingInvalid)
}

func TestSharedSettlementSnapshotsMultiplierAndBase(t *testing.T) {
	groupID := int64(3)
	account := &Account{Extra: map[string]any{SharedPoolOwnerKey: int64(9), SharedPoolDispatchConsentKey: true}, SharedPoolSettlement: &SharedPoolSettlementTerms{Multiplier: .5, PlatformRateBPS: 500, ProxyRateBPS: 100}}
	cmd := &UsageBillingCommand{}
	applySharedPoolBillingSnapshot(cmd, account, &APIKey{GroupID: &groupID, Group: &Group{SubscriptionType: SubscriptionTypeStandard}}, &UsageLog{TotalCost: 10})
	require.NotNil(t, cmd.SharedPoolSettlementMultiplier)
	require.Equal(t, .5, *cmd.SharedPoolSettlementMultiplier)
	require.Equal(t, 10.0, cmd.SharedPoolBaseCost)
	account.SharedPoolSettlement.Multiplier = 9.0
	account.SharedPoolSettlement.PlatformRateBPS = 900
	require.Equal(t, .5, *cmd.SharedPoolSettlementMultiplier)
	require.Equal(t, 500, *cmd.SharedPoolPlatformRateBPS)
	require.Equal(t, 100, *cmd.SharedPoolProxyRateBPS)
	second := *cmd
	other := .7
	second.SharedPoolSettlementMultiplier = &other
	cmd.Normalize()
	second.Normalize()
	require.NotEqual(t, cmd.RequestFingerprint, second.RequestFingerprint)
}
