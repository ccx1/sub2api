package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAstraGroupSelectionDoesNotTrustPreview(t *testing.T) {
	value := AstraRoutingSettings{CookiePool: CodexGatewayPinConfig{Enabled: true, SourceSelection: "groups", TargetSelection: "groups", SourceGroupIDs: []int64{1}, TargetGroupIDs: []int64{2}, SourceAccountIDs: []int64{99}, TargetAccountIDs: []int64{99}}}
	resolved, err := ResolveAstraDependencies(value)
	require.NoError(t, err, "client previews are not the authority for group membership")
	stored := AstraStoredSettings(resolved)
	require.Empty(t, stored.CookiePool.SourceAccountIDs)
	require.Empty(t, stored.CookiePool.TargetAccountIDs)
	require.Equal(t, []int64{1}, stored.CookiePool.SourceGroupIDs)
	resolved.CookiePool.SourceGroupIDs[0] = 10
	require.Equal(t, int64(1), value.CookiePool.SourceGroupIDs[0])
	value.CookiePool.SourceSelection = "unknown"
	require.ErrorContains(t, value.Validate(), "astra_selection_invalid")
}

func TestAstraResolvedGroupSelectionBoundaries(t *testing.T) {
	value := AstraRoutingSettings{CookiePool: CodexGatewayPinConfig{Enabled: true, TargetSelection: "groups", SourceAccountIDs: []int64{1}, TargetAccountIDs: []int64{2}}, WSSession: CodexWSAnchorConfig{Enabled: true, AccountIDs: []int64{3}}}
	require.ErrorContains(t, ValidateAstraResolvedAccounts(value), "astra_ws_outside_targets")
	value.WSSession.AccountIDs = []int64{1, 2}
	require.NoError(t, ValidateAstraResolvedAccounts(value))
	value.CookiePool.TargetAccountIDs = []int64{1}
	require.ErrorContains(t, ValidateAstraResolvedAccounts(value), "astra_account_overlap")
	value.CookiePool.TargetAccountIDs = nil
	require.ErrorContains(t, ValidateAstraResolvedAccounts(value), "astra_target_required")
	value.CookiePool.TargetAccountIDs = make([]int64, 65)
	require.ErrorContains(t, ValidateAstraResolvedAccounts(value), "astra_group_accounts_limit")
}

func TestAstraGroupSelectionCannotRemoveItsOwnMembership(t *testing.T) {
	value := AstraRoutingSettings{AccountScheduling: true, SchedulingMode: "groups", SchedulingGroupIDs: []int64{2}, CookiePool: CodexGatewayPinConfig{Enabled: true, SourceAccountIDs: []int64{1}, TargetSelection: "groups", TargetGroupIDs: []int64{2}}}
	require.ErrorContains(t, value.Validate(), "astra_selection_scheduling_conflict")
	value.SchedulingGroupIDs = []int64{3}
	require.NoError(t, value.Validate())
	value.SchedulingGroupIDs = []int64{2}
	value.CookiePool.Enabled = false
	require.NoError(t, value.Validate(), "disabling an invalid policy must remain possible")
}

func TestAstraSourceGroupsResolveBeforeWSExpansion(t *testing.T) {
	value := AstraRoutingSettings{CookiePool: CodexGatewayPinConfig{Enabled: true, SourceSelection: "groups", SourceGroupIDs: []int64{1}, TargetAccountIDs: []int64{20}}, WSSession: CodexWSAnchorConfig{Enabled: true, AccountIDs: []int64{10, 30}}}
	resolved, err := ResolveAstraDependencies(value)
	require.NoError(t, err)
	require.Equal(t, []int64{20}, resolved.CookiePool.TargetAccountIDs)
	resolved.CookiePool.SourceAccountIDs = []int64{10}
	resolved, err = ResolveAstraDependencies(resolved)
	require.NoError(t, err)
	require.Equal(t, []int64{20, 30}, resolved.CookiePool.TargetAccountIDs)
}
