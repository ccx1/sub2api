package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolLegacyPlazaVisibilityUsesOrdinaryRestrictions(t *testing.T) {
	groups := []service.PlazaGroup{{ID: 1, IsSharedPool: true}, {ID: 2}}
	visible := filterPlazaVisibleGroups(groups, nil, false)
	require.Len(t, visible, 2)
	visible = filterPlazaVisibleGroups(groups, map[int64]struct{}{}, true)
	require.Empty(t, visible)
	visible = filterPlazaVisibleGroups(groups, map[int64]struct{}{1: {}}, true)
	require.Len(t, visible, 1)
	require.EqualValues(t, 1, visible[0].ID)
}
