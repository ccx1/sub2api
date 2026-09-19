//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestDailyCooldownRepositoryRejectsInvalidExtraBeforePersistence(t *testing.T) {
	repo := &accountRepository{}
	extra := map[string]any{service.DailyCooldownExtraKey: map[string]any{
		"enabled": true, "start": "23:00", "end": "23:00", "timezone": "Asia/Shanghai",
	}}
	require.Error(t, repo.UpdateExtra(context.Background(), 19, extra))
	_, err := repo.BulkUpdate(context.Background(), []int64{19}, service.AccountBulkUpdate{Extra: extra})
	require.Error(t, err)
}

func TestDailyCooldownCreateRejectsInvalidConfigBeforePersistence(t *testing.T) {
	for _, extra := range []map[string]any{
		{service.DailyCooldownExtraKey: map[string]any{"enabled": true, "start": "23:00", "end": "23:00"}},
		{"random_proxy_max_reuse_minutes": -1},
		{"random_proxy_max_reuse_minutes": 1.5},
	} {
		account := &service.Account{Extra: extra}
		require.Error(t, createAccountRecord(context.Background(), nil, account))
	}
}

func TestDailyCooldownSchedulerProjectionRetainsSchedule(t *testing.T) {
	account := service.Account{ID: 19, Status: service.StatusActive, Schedulable: true, Extra: map[string]any{
		service.DailyCooldownExtraKey: map[string]any{
			"enabled": true, "start": "23:00", "end": "08:00", "timezone": "Asia/Shanghai",
		},
		"unused_large_field": "discarded",
	}}
	projected := buildSchedulerMetadataAccount(account)
	payload, err := json.Marshal(projected)
	require.NoError(t, err)
	var cached service.Account
	require.NoError(t, json.Unmarshal(payload, &cached))
	start := time.Date(2026, 9, 19, 23, 0, 0, 0, time.FixedZone("CST", 8*3600))
	require.True(t, cached.IsInDailyCooldown(start))
	require.False(t, cached.IsInDailyCooldown(start.Add(9*time.Hour)))
	require.Equal(t, service.StatusActive, cached.Status)
	require.True(t, cached.Schedulable)
	require.NotContains(t, cached.Extra, "unused_large_field")
}

func TestRandomProxyReuseSchedulerProjectionPreservesPeriod(t *testing.T) {
	for _, minutes := range []int{0, 180, 525600} {
		account := service.Account{Extra: map[string]any{"random_proxy_max_reuse_minutes": minutes}}
		projected := buildSchedulerMetadataAccount(account)
		require.Equal(t, minutes, projected.Extra["random_proxy_max_reuse_minutes"])
	}
}
