package service

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestSplitSharedPoolBillingAmountConservesMoney(t *testing.T) {
	for _, amount := range []float64{0, 0.00000001, 0.00000005, 0.000078125, 5, 12345.98765432} {
		for _, rates := range [][2]int{{0, 0}, {2000, 100}, {9999, 1}, {3333, 0}} {
			platform, owner, err := SplitSharedPoolBillingAmount(amount, rates[0], rates[1])
			require.NoError(t, err)
			require.True(t, platform.Add(owner).Equal(decimal.NewFromFloat(amount).Round(8)))
			require.False(t, platform.IsNegative())
			require.False(t, owner.IsNegative())
		}
	}
	platform, owner, err := SplitSharedPoolBillingAmount(5, 2000, 100)
	require.NoError(t, err)
	require.Equal(t, "1.05000000", platform.StringFixed(8))
	require.Equal(t, "3.95000000", owner.StringFixed(8))
}

func TestSplitSharedPoolBillingAmountRejectsInvalid(t *testing.T) {
	for _, amount := range []float64{-1, math.NaN(), math.Inf(1)} {
		_, _, err := SplitSharedPoolBillingAmount(amount, 2000, 100)
		require.ErrorIs(t, err, ErrSharedPoolBillingInvalid)
	}
	for _, rates := range [][2]int{{-1, 0}, {0, -1}, {10001, 0}, {9901, 100}, {0, math.MaxInt}} {
		_, _, err := SplitSharedPoolBillingAmount(1, rates[0], rates[1])
		require.ErrorIs(t, err, ErrSharedPoolBillingInvalid)
	}
}

func TestSharedPoolBillingSnapshotUsesActualProxyAndEffectiveGroup(t *testing.T) {
	owner, proxy, keyGroup, effectiveGroup := int64(42), int64(9), int64(10), int64(11)
	account := &Account{Extra: map[string]any{"shared_pool_owner_id": owner, ProxyModeExtraKey: ProxyModeRandom}}
	cmd := &UsageBillingCommand{}
	key := &APIKey{GroupID: &keyGroup, Group: &Group{IsSharedPool: true, SubscriptionType: SubscriptionTypeStandard}}
	applySharedPoolBillingSnapshot(cmd, account, key, &UsageLog{GroupID: &effectiveGroup})
	require.Equal(t, owner, cmd.SharedPoolOwnerID)
	require.Equal(t, effectiveGroup, cmd.GroupID)
	require.True(t, cmd.SharedPoolGroup)
	key.Group.IsSharedPool = false
	require.True(t, cmd.SharedPoolGroup, "请求快照不受后续分组开关影响")
	require.False(t, cmd.UsesPlatformProxy, "空池直连不得收平台代理附加分成")
	account.ProxyID = &proxy
	applySharedPoolBillingSnapshot(cmd, account, nil, nil)
	require.True(t, cmd.UsesPlatformProxy)
	proxy = 100
	require.Equal(t, int64(9), *cmd.SharedPoolProxyID, "快照不能保留可变指针")
	delete(account.Extra, ProxyModeExtraKey)
	cmd = &UsageBillingCommand{}
	applySharedPoolBillingSnapshot(cmd, account, nil, nil)
	require.False(t, cmd.UsesPlatformProxy, "用户固定代理不收附加分成")
}

func TestSharedPoolBillingSnapshotRequiresSharedStandardGroup(t *testing.T) {
	account := &Account{Extra: map[string]any{"shared_pool_owner_id": int64(42)}}
	for _, group := range []*Group{nil, {}, {IsSharedPool: true, SubscriptionType: SubscriptionTypeSubscription}} {
		cmd := &UsageBillingCommand{}
		applySharedPoolBillingSnapshot(cmd, account, &APIKey{Group: group}, nil)
		require.False(t, cmd.SharedPoolGroup)
	}
}

func TestSharedPoolOwnerSnapshotAndFingerprint(t *testing.T) {
	for _, value := range []any{int64(42), 42, float64(42), json.Number("42"), "42"} {
		require.Equal(t, int64(42), sharedPoolBillingOwnerID(value))
	}
	for _, value := range []any{42.5, math.NaN(), math.Inf(1), true, "not-an-id"} {
		require.Zero(t, sharedPoolBillingOwnerID(value))
	}
	a := &UsageBillingCommand{RequestID: "test", UserID: 1, SharedPoolOwnerID: 42, GroupID: 3}
	b := *a
	b.GroupID = 4
	a.Normalize()
	b.Normalize()
	require.NotEqual(t, a.RequestFingerprint, b.RequestFingerprint)
	c := *a
	c.RequestFingerprint = ""
	c.SharedPoolGroup = true
	c.Normalize()
	require.NotEqual(t, a.RequestFingerprint, c.RequestFingerprint, "共享分组快照参与幂等指纹")
}
