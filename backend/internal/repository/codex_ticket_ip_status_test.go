package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyPoolAllocatorListCodexIPStatusFiltersAndPaginates(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0)
	ctx := context.Background()
	now := time.Now().UnixMilli()
	seed := func(ip string, value map[string]any) {
		payload, err := json.Marshal(value)
		require.NoError(t, err)
		require.NoError(t, a.rdb.Set(ctx, proxyIPGuardKey(ip), payload, 0).Err())
	}
	seed("203.0.113.10", map[string]any{"ip": "203.0.113.10", "until_at": now + 60_000, "rounds": 2, "failed": map[string]int64{"1": now - 1000}, "disabled": false, "last_failure_at": now - 1000})
	seed("203.0.113.11", map[string]any{"ip": "203.0.113.11", "until_at": 0, "rounds": 3, "failed": map[string]int{}, "failed_accounts": 2, "disabled": true, "last_failure_at": now - 2000})
	seed("203.0.113.12", map[string]any{"ip": "203.0.113.12", "until_at": now - 1000, "rounds": 1, "failed": map[string]int{}, "disabled": false})
	seed("203.0.113.13", map[string]any{"until_at": now + 60_000, "rounds": 1, "failed": map[string]int{}, "disabled": false})

	items, total, err := a.ListCodexIPStatus(ctx, "", 1, 1)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	require.Len(t, items, 1)
	require.Equal(t, service.CodexIPStatusCooling, items[0].Status)
	require.Equal(t, "203.0.113.10", items[0].IP)
	require.Equal(t, 1, items[0].FailedAccounts)

	items, total, err = a.ListCodexIPStatus(ctx, service.CodexIPStatusDisabled, 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Len(t, items, 1)
	require.Equal(t, "203.0.113.11", items[0].IP)
	require.Equal(t, service.CodexIPStatusDisabled, items[0].Status)
	require.Equal(t, 2, items[0].FailedAccounts)

	items, total, err = a.ListCodexIPStatus(ctx, service.CodexIPStatusCooling, 2, 20)
	require.NoError(t, err)
	require.EqualValues(t, 1, total)
	require.Empty(t, items)
}

func TestProxyPoolAllocatorListCodexIPStatusRejectsInvalidStatus(t *testing.T) {
	a, _ := newProxyPoolAllocatorTest(t, 0)
	_, _, err := a.ListCodexIPStatus(context.Background(), "invalid", 1, 20)
	require.Error(t, err)
}
