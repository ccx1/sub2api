package service

import (
	"context"
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateRandomProxyReuseExtra(t *testing.T) {
	for _, value := range []any{nil, 0, 60, int64(120), float64(525600), json.Number("1440")} {
		require.NoError(t, ValidateRandomProxyReuseExtra(map[string]any{RandomProxyMaxReuseMinutesExtraKey: value}), "%v", value)
	}
	for _, value := range []any{-1, 525601, 0.5, "60", true, []int{60}, math.Inf(1), json.Number("1e999")} {
		require.Error(t, ValidateRandomProxyReuseExtra(map[string]any{RandomProxyMaxReuseMinutesExtraKey: value}), "%v", value)
	}
	require.NoError(t, ValidateRandomProxyReuseExtra(nil))
	var absent *Account
	require.Zero(t, absent.RandomProxyMaxReuseDuration())
}

func TestResolveRandomProxyForwardsReusePeriod(t *testing.T) {
	account := randomProxyAccount(RandomProxyEmptyPoolPolicyReject)
	account.Extra[RandomProxyMaxReuseMinutesExtraKey] = float64(120)
	repo := &balancedAccountProxyStub{pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{
		proxy: &Proxy{ID: 7, Status: StatusActive},
	}}
	require.NoError(t, ResolveRandomProxy(context.Background(), account, repo))
	require.Equal(t, 2*time.Hour, repo.selections[0].MaxReuseDuration)
	require.Equal(t, int64(42), repo.selections[0].AccountID)
}
