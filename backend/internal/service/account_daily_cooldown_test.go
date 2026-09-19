//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func dailyCooldownExtra(start, end, timezone string) map[string]any {
	return map[string]any{"daily_cooldown": map[string]any{
		"enabled": true, "start": start, "end": end, "timezone": timezone,
	}}
}

func TestAccountDailyCooldownBoundaries(t *testing.T) {
	account := &Account{Extra: dailyCooldownExtra("23:00", "08:00", "Asia/Shanghai")}
	for _, tc := range []struct {
		at   string
		want bool
	}{
		{"2026-09-19T22:59:59+08:00", false},
		{"2026-09-19T23:00:00+08:00", true},
		{"2026-09-20T00:00:00+08:00", true},
		{"2026-09-20T07:59:59+08:00", true},
		{"2026-09-20T08:00:00+08:00", false},
		{"2026-09-20T15:00:00Z", true},
	} {
		t.Run(tc.at, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, tc.at)
			require.NoError(t, err)
			require.Equal(t, tc.want, account.IsInDailyCooldown(now))
		})
	}
}

func TestAccountDailyCooldownDaytimeAndDefaultTimezone(t *testing.T) {
	account := &Account{Extra: dailyCooldownExtra("09:30", "12:00", "")}
	delete(account.Extra["daily_cooldown"].(map[string]any), "timezone")
	for _, tc := range []struct {
		hour, minute int
		want         bool
	}{{1, 29, false}, {1, 30, true}, {3, 59, true}, {4, 0, false}} {
		now := time.Date(2026, 9, 19, tc.hour, tc.minute, 0, 0, time.UTC)
		require.Equal(t, tc.want, account.IsInDailyCooldown(now))
	}
}

func TestAccountDailyCooldownDSTUsesLocalClock(t *testing.T) {
	account := &Account{Extra: dailyCooldownExtra("01:00", "02:00", "America/New_York")}
	for _, hour := range []int{5, 6} {
		now := time.Date(2026, 11, 1, hour, 30, 0, 0, time.UTC)
		require.True(t, account.IsInDailyCooldown(now), "both repeated 01:30 hours must cool down")
	}
	require.False(t, account.IsInDailyCooldown(time.Date(2026, 11, 1, 7, 0, 0, 0, time.UTC)))
}

func TestValidateDailyCooldownExtra(t *testing.T) {
	for _, extra := range []map[string]any{
		nil, {}, {"daily_cooldown": nil},
		{"daily_cooldown": map[string]any{"enabled": false}},
		dailyCooldownExtra("23:00", "08:00", "Asia/Shanghai"),
	} {
		require.NoError(t, ValidateDailyCooldownExtra(extra))
	}
	for _, tc := range []struct {
		name string
		key  string
		bad  any
	}{
		{"non boolean", "enabled", "true"},
		{"missing start", "start", nil},
		{"short hour", "start", "9:00"},
		{"invalid hour", "start", "24:00"},
		{"invalid minute", "start", "23:60"},
		{"whitespace", "start", " 23:00"},
		{"seconds", "start", "23:00:00"},
		{"equal bounds", "end", "23:00"},
		{"invalid timezone", "timezone", "Mars/Unknown"},
		{"host timezone", "timezone", "Local"},
		{"non string timezone", "timezone", 8},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extra := dailyCooldownExtra("23:00", "08:00", "Asia/Shanghai")
			extra["daily_cooldown"].(map[string]any)[tc.key] = tc.bad
			require.Error(t, ValidateDailyCooldownExtra(extra))
		})
	}
	require.Error(t, ValidateDailyCooldownExtra(map[string]any{"daily_cooldown": true}))
}

func TestDailyCooldownDisabledAndMalformedDoNotBlock(t *testing.T) {
	now := time.Now()
	for _, account := range []*Account{nil, {}, {Extra: map[string]any{
		"daily_cooldown": map[string]any{"enabled": false},
	}}, {Extra: dailyCooldownExtra("invalid", "08:00", "Asia/Shanghai")}} {
		require.False(t, account.IsInDailyCooldown(now))
	}
}

func TestDailyCooldownSchedulingRestoresOnlyEligibleAccounts(t *testing.T) {
	start := time.Date(2026, 9, 19, 23, 0, 0, 0, time.FixedZone("CST", 8*3600))
	end := start.Add(9 * time.Hour)
	for _, tc := range []struct {
		name   string
		change func(*Account)
		want   bool
	}{
		{"active", func(*Account) {}, true},
		{"manual pause", func(a *Account) { a.Schedulable = false }, false},
		{"disabled", func(a *Account) { a.Status = StatusDisabled }, false},
		{"error", func(a *Account) { a.Status = StatusError }, false},
		{"rate limited", func(a *Account) { until := end.Add(time.Hour); a.RateLimitResetAt = &until }, false},
		{"overloaded", func(a *Account) { until := end.Add(time.Hour); a.OverloadUntil = &until }, false},
		{"temporary blocked", func(a *Account) { until := end.Add(time.Hour); a.TempUnschedulableUntil = &until }, false},
		{"expired", func(a *Account) { a.ExpiresAt = &start; a.AutoPauseOnExpired = true }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &Account{Status: StatusActive, Schedulable: true, Extra: dailyCooldownExtra("23:00", "08:00", "Asia/Shanghai")}
			tc.change(a)
			status, schedulable := a.Status, a.Schedulable
			require.False(t, a.isSchedulableAt(start))
			require.Equal(t, tc.want, a.isSchedulableAt(end))
			require.Equal(t, status, a.Status)
			require.Equal(t, schedulable, a.Schedulable)
		})
	}
}

func TestDailyCooldownBlocksShadowCredentialUse(t *testing.T) {
	now := time.Now().UTC()
	a := &Account{Status: StatusActive, Schedulable: true, Extra: dailyCooldownExtra(
		now.Add(-time.Hour).Format("15:04"), now.Add(time.Hour).Format("15:04"), "UTC",
	)}
	require.False(t, a.IsSchedulable())
	require.False(t, a.IsCredentialUsableForShadow())
	a.Extra["daily_cooldown"].(map[string]any)["enabled"] = false
	require.True(t, a.IsSchedulable())
	require.True(t, a.IsCredentialUsableForShadow())
}
