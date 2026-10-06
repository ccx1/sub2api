package config

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestAstraAutomaticDependencies(t *testing.T) {
	v := AstraRoutingSettings{CookiePool: CodexGatewayPinConfig{SourceAccountIDs: []int64{299}}, WSSession: CodexWSAnchorConfig{Enabled: true, AccountIDs: []int64{299, 300}}}
	resolved, err := ResolveAstraDependencies(v)
	require.NoError(t, err)
	require.True(t, resolved.CookiePool.Enabled)
	require.Equal(t, []int64{300}, resolved.CookiePool.TargetAccountIDs)
	require.False(t, v.CookiePool.Enabled)
	require.Empty(t, v.CookiePool.TargetAccountIDs)
	resolved.CookiePool.SourceAccountIDs[0] = 1
	require.Equal(t, int64(299), v.CookiePool.SourceAccountIDs[0])
	v.CookiePool.SourceAccountIDs = nil
	_, err = ResolveAstraDependencies(v)
	require.ErrorContains(t, err, "astra_source_required")
	v.WSSession.Enabled = false
	_, err = ResolveAstraDependencies(v)
	require.NoError(t, err)
}

func TestAstraRotationRequiresAffinityAndBoundsAttempts(t *testing.T) {
	v := AstraRoutingSettings{CookiePool: CodexGatewayPinConfig{RotateNodes: true, MaxNodeAttempts: 3}}
	resolved, err := ResolveAstraDependencies(v)
	require.NoError(t, err)
	require.True(t, resolved.CookiePool.IPAffinity)
	require.False(t, v.CookiePool.IPAffinity)
	for _, invalid := range []int{-1, 11} {
		v.CookiePool.MaxNodeAttempts = invalid
		_, err = ResolveAstraDependencies(v)
		require.Error(t, err)
	}
}

func TestAstraNodeCooldownBounds(t *testing.T) {
	for _, seconds := range []int{-1, 1, 59, 86401} {
		require.Error(t, (CodexGatewayPinConfig{NodeCooldownSeconds: seconds}).Validate())
	}
	for _, seconds := range []int{0, 60, 3600, 86400} {
		require.NoError(t, (CodexGatewayPinConfig{NodeCooldownSeconds: seconds}).Validate())
	}
}

func TestAstraAutomaticSourcesTakePriorityOverTargets(t *testing.T) {
	for _, ws := range []bool{false, true} {
		value := AstraRoutingSettings{CookiePool: CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{1, 3}, TargetAccountIDs: []int64{3, 2, 1, 4}}, WSSession: CodexWSAnchorConfig{Enabled: ws, AccountIDs: []int64{1, 2}}}
		resolved, err := ResolveAstraDependencies(value)
		require.NoError(t, err)
		require.Equal(t, []int64{2, 4}, resolved.CookiePool.TargetAccountIDs)
		require.Equal(t, []int64{1, 3}, resolved.CookiePool.SourceAccountIDs)
		require.Equal(t, []int64{3, 2, 1, 4}, value.CookiePool.TargetAccountIDs, "normalization must not mutate the caller")
	}
}

func TestAstraAutomaticExcludingSourcesRequiresRemainingTarget(t *testing.T) {
	value := AstraRoutingSettings{CookiePool: CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{1}, TargetAccountIDs: []int64{1}}}
	_, err := ResolveAstraDependencies(value)
	require.ErrorContains(t, err, "astra_target_required")
	value.CookiePool.Enabled = false
	resolved, err := ResolveAstraDependencies(value)
	require.NoError(t, err)
	require.Empty(t, resolved.CookiePool.TargetAccountIDs)
}
