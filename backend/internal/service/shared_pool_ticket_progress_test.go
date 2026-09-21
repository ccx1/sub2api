package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

var sharedTicketProgressNow = time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC)

func sharedTicketProgressAccount() *Account {
	return &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
		Extra: map[string]any{}, Credentials: map[string]any{"access_token": "credential-sentinel"}}
}

func sharedTicketProgressRaw(length int, expires time.Time) map[string]any {
	return map[string]any{"state": fakeCodexTicketState(length), "length": length, "expires_at": expires}
}

func sharedTicketProgressCapacity(t *testing.T, account *Account) *SharedPoolCapacity {
	t.Helper()
	snapshot := NewSharedPoolTicketAccountSnapshot(account, sharedTicketProgressNow)
	require.NotNil(t, snapshot)
	snapshot.Available, snapshot.Concurrency = true, 3
	return &SharedPoolCapacity{TotalAccounts: 9, AvailableAccounts: 1, ConcurrencyCapacity: 3, TicketAccounts: []SharedPoolTicketAccountSnapshot{*snapshot}}
}

func requireSharedTicketAvailability(t *testing.T, got SharedPoolCapacity, available bool) {
	t.Helper()
	want := int64(0)
	if available {
		want = 1
	}
	require.Equal(t, want, got.AvailableAccounts)
	require.Equal(t, want*3, got.ConcurrencyCapacity)
	require.EqualValues(t, 9, got.TotalAccounts)
}

func TestSharedPoolTicketSnapshotEligibility(t *testing.T) {
	require.Nil(t, NewSharedPoolTicketAccountSnapshot(nil, sharedTicketProgressNow))
	for _, tc := range []struct {
		name   string
		change func(*Account)
		want   bool
	}{
		{"oauth default enabled", func(*Account) {}, true},
		{"setup token", func(a *Account) { a.Type = AccountTypeSetupToken }, true},
		{"nil extra", func(a *Account) { a.Extra = nil }, true},
		{"explicit enabled", func(a *Account) { a.Extra[OpenAICodexTicketEnabledExtraKey] = true }, true},
		{"null enabled", func(a *Account) { a.Extra[OpenAICodexTicketEnabledExtraKey] = nil }, true},
		{"legacy nonboolean enabled", func(a *Account) { a.Extra[OpenAICodexTicketEnabledExtraKey] = "false" }, true},
		{"explicit disabled", func(a *Account) { a.Extra[OpenAICodexTicketEnabledExtraKey] = false }, false},
		{"api key", func(a *Account) { a.Type = AccountTypeAPIKey }, false},
		{"other platform", func(a *Account) { a.Platform = PlatformAnthropic }, false},
		{"shadow", func(a *Account) { parent := int64(1); a.ParentAccountID = &parent }, false},
		{"disabled account", func(a *Account) { a.Status = StatusDisabled }, false},
		{"error account", func(a *Account) { a.Status = StatusError }, false},
		{"missing status", func(a *Account) { a.Status = "" }, false},
		{"temporarily unschedulable", func(a *Account) { a.Schedulable = false }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := sharedTicketProgressAccount()
			tc.change(account)
			require.Equal(t, tc.want, NewSharedPoolTicketAccountSnapshot(account, sharedTicketProgressNow) != nil)
		})
	}
}

func TestSharedPoolTicketCatalogGlobalSwitchAndFailOpen(t *testing.T) {
	capacity := sharedTicketProgressCapacity(t, sharedTicketProgressAccount())
	for _, cfg := range []config.OpenAICodexTicketConfig{
		{FailClosed: true}, {Enabled: true}, {Enabled: true, FailClosed: true, Models: []string{"", " \t"}},
	} {
		require.Equal(t, *capacity, GetSharedPoolCatalogCapacity(capacity, cfg, sharedTicketProgressNow))
	}
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}
	require.Equal(t, SharedPoolCapacity{}, GetSharedPoolCatalogCapacity(nil, cfg, sharedTicketProgressNow))
	legacy := &SharedPoolCapacity{TotalAccounts: 9, AvailableAccounts: 2, ConcurrencyCapacity: 5}
	require.Equal(t, *legacy, GetSharedPoolCatalogCapacity(legacy, cfg, sharedTicketProgressNow))
	requireSharedTicketAvailability(t, GetSharedPoolCatalogCapacity(capacity, cfg, sharedTicketProgressNow), false)
	require.EqualValues(t, 1, capacity.AvailableAccounts, "返回副本，不得扣减来源汇总")
	require.EqualValues(t, 3, capacity.ConcurrencyCapacity)
}

func TestSharedPoolTicketCatalogConfiguredModels(t *testing.T) {
	account := sharedTicketProgressAccount()
	for _, model := range []string{openAICodexTicketDefaultModel, openAICodexTicketDefaultSolModel, "custom-model"} {
		account.Extra[openAICodexTicketExtraKey(model)] = sharedTicketProgressRaw(292, sharedTicketProgressNow.Add(time.Hour))
	}
	capacity := sharedTicketProgressCapacity(t, account)
	for _, tc := range []struct {
		name   string
		models []string
		want   bool
	}{
		{"defaults nil", nil, true}, {"defaults empty", []string{}, true},
		{"custom", []string{"custom-model"}, true}, {"missing custom", []string{"missing"}, false},
		{"blank and duplicates", []string{" custom-model ", "", "custom-model", "\t"}, true},
		{"all blank", []string{"", " \t "}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: tc.models}
			requireSharedTicketAvailability(t, GetSharedPoolCatalogCapacity(capacity, cfg, sharedTicketProgressNow), tc.want)
		})
	}
}

func TestSharedPoolTicketCatalogTargetLength(t *testing.T) {
	for _, tc := range []struct {
		name           string
		length, target int
		ready          bool
	}{
		{"custom target", 332, 332, true}, {"custom shape wrong target", 312, 292, false},
		{"default target", 292, 0, true}, {"negative target fallback", 292, -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := sharedTicketProgressAccount()
			account.Extra[openAICodexTicketExtraKey("custom")] = sharedTicketProgressRaw(tc.length, sharedTicketProgressNow.Add(time.Hour))
			cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: []string{"custom"}, TargetLength: tc.target}
			requireSharedTicketAvailability(t, GetSharedPoolCatalogCapacity(sharedTicketProgressCapacity(t, account), cfg, sharedTicketProgressNow), tc.ready)
		})
	}
}

func TestSharedPoolTicketSnapshotRejectsMalformedTickets(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any) any
		ready  bool
	}{
		{"valid", func(raw map[string]any) any { return raw }, true},
		{"zero length inferred", func(raw map[string]any) any { raw["length"] = 0; return raw }, true},
		{"missing length inferred", func(raw map[string]any) any { delete(raw, "length"); return raw }, true},
		{"state whitespace trimmed", func(raw map[string]any) any { raw["state"] = " \t" + raw["state"].(string) + "\n"; return raw }, true},
		{"invalid prefix", func(raw map[string]any) any { raw["state"] = strings.Repeat("X", 292); return raw }, false},
		{"declared length differs", func(raw map[string]any) any { raw["length"] = 291; return raw }, false},
		{"negative length", func(raw map[string]any) any { raw["length"] = -1; return raw }, false},
		{"empty state", func(raw map[string]any) any { raw["state"] = ""; return raw }, false},
		{"zero expiry", func(raw map[string]any) any { raw["expires_at"] = time.Time{}; return raw }, false},
		{"invalid expiry", func(raw map[string]any) any { raw["expires_at"] = "invalid"; return raw }, false},
		{"invalid payload", func(map[string]any) any { return "not-an-object" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := sharedTicketProgressAccount()
			account.Extra[openAICodexTicketExtraKey("model")] = tc.change(sharedTicketProgressRaw(292, sharedTicketProgressNow.Add(time.Hour)))
			cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: []string{"model"}}
			requireSharedTicketAvailability(t, GetSharedPoolCatalogCapacity(sharedTicketProgressCapacity(t, account), cfg, sharedTicketProgressNow), tc.ready)
		})
	}
}

func TestSharedPoolTicketSnapshotRejectsNoncanonicalExtraKeys(t *testing.T) {
	for _, key := range []string{openAICodexTicketExtraKeyPrefix + " model", openAICodexTicketExtraKeyPrefix + "model ", openAICodexTicketExtraKeyPrefix + "", "model", "codex_harvest_proxy_url"} {
		t.Run(key, func(t *testing.T) {
			account := sharedTicketProgressAccount()
			account.Extra[key] = sharedTicketProgressRaw(292, sharedTicketProgressNow.Add(time.Hour))
			capacity := sharedTicketProgressCapacity(t, account)
			require.Empty(t, capacity.TicketAccounts[0].tickets)
			got := GetSharedPoolCatalogCapacity(capacity, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: []string{"model"}}, sharedTicketProgressNow)
			requireSharedTicketAvailability(t, got, false)
		})
	}
}

func TestSharedPoolTicketCatalogExpiryAndRefreshBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name           string
		expiresAfter   time.Duration
		refreshSeconds int
		ready          bool
	}{
		{"expired", -time.Nanosecond, 0, false}, {"expires now", 0, 0, false},
		{"still valid", time.Nanosecond, 0, true},
		{"inside default window", 10*time.Minute - time.Nanosecond, 0, true},
		{"default window boundary", 10 * time.Minute, 0, true},
		{"outside default window", 10*time.Minute + time.Nanosecond, 0, true},
		{"custom window boundary", 30 * time.Second, 30, true},
		{"outside custom window", 30*time.Second + time.Nanosecond, 30, true},
		{"negative refresh fallback", 10 * time.Minute, -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := sharedTicketProgressAccount()
			account.Extra[openAICodexTicketExtraKey("model")] = sharedTicketProgressRaw(292, sharedTicketProgressNow.Add(tc.expiresAfter))
			cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: []string{"model"}, RefreshBeforeSeconds: tc.refreshSeconds}
			requireSharedTicketAvailability(t, GetSharedPoolCatalogCapacity(sharedTicketProgressCapacity(t, account), cfg, sharedTicketProgressNow), tc.ready)
		})
	}
}

func TestSharedPoolTicketCatalogRechecksSnapshotExpiry(t *testing.T) {
	account := sharedTicketProgressAccount()
	account.Extra[openAICodexTicketExtraKey("model")] = sharedTicketProgressRaw(292, sharedTicketProgressNow.Add(time.Hour))
	capacity := sharedTicketProgressCapacity(t, account)
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: []string{"model"}}
	got := GetSharedPoolCatalogCapacity(capacity, cfg, sharedTicketProgressNow.Add(time.Hour))
	requireSharedTicketAvailability(t, got, false)
}

func TestSharedPoolTicketCatalogKeepsPartiallyReadyAndRenewingAccount(t *testing.T) {
	account := sharedTicketProgressAccount()
	account.Extra[openAICodexTicketExtraKey(openAICodexTicketDefaultModel)] = sharedTicketProgressRaw(292, sharedTicketProgressNow.Add(time.Minute))
	capacity := sharedTicketProgressCapacity(t, account)
	for _, models := range [][]string{nil, {openAICodexTicketDefaultSolModel, openAICodexTicketDefaultModel}} {
		cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: models}
		requireSharedTicketAvailability(t, GetSharedPoolCatalogCapacity(capacity, cfg, sharedTicketProgressNow), true)
	}
	account.Extra[openAICodexTicketExtraKey(openAICodexTicketDefaultSolModel)] = sharedTicketProgressRaw(292, sharedTicketProgressNow.Add(time.Hour))
	capacity = sharedTicketProgressCapacity(t, account)
	requireSharedTicketAvailability(t, GetSharedPoolCatalogCapacity(capacity, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, sharedTicketProgressNow), true)
}

func TestSharedPoolTicketCatalogAggregatesAccountsWithoutUsingPoolTotal(t *testing.T) {
	capacity := &SharedPoolCapacity{TotalAccounts: 9, AvailableAccounts: 4, ConcurrencyCapacity: 12}
	for _, ttl := range []time.Duration{0, -time.Second, time.Minute, time.Hour} {
		account := sharedTicketProgressAccount()
		account.Extra[openAICodexTicketExtraKey("model")] = sharedTicketProgressRaw(292, sharedTicketProgressNow.Add(ttl))
		snapshot := NewSharedPoolTicketAccountSnapshot(account, sharedTicketProgressNow)
		snapshot.Available, snapshot.Concurrency = true, 3
		capacity.TicketAccounts = append(capacity.TicketAccounts, *snapshot)
	}
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: []string{"model", "model"}}
	got := GetSharedPoolCatalogCapacity(capacity, cfg, sharedTicketProgressNow)
	require.EqualValues(t, 2, got.AvailableAccounts)
	require.EqualValues(t, 6, got.ConcurrencyCapacity)
	require.EqualValues(t, 9, got.TotalAccounts)
}

func TestSharedPoolTicketCatalogDeductsOnlyAvailableCapacity(t *testing.T) {
	for _, tc := range []struct {
		name                             string
		concurrency                      int
		available, ready                 bool
		unlimited, wantAccounts, wantCap int64
		wantUnlimited                    int64
	}{
		{"unavailable ignored", 100, false, false, 0, 4, 8, 0},
		{"finite removed", 3, true, false, 0, 3, 5, 0},
		{"last unlimited removed", 0, true, false, 1, 3, 8, 0},
		{"nonparticipating unlimited retained", 0, true, false, 2, 3, 8, 1},
		{"negative concurrency unlimited", -1, true, false, 2, 3, 8, 1},
		{"ready unlimited retained", 0, true, true, 1, 4, 8, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := sharedTicketProgressAccount()
			if tc.ready {
				account.Extra[openAICodexTicketExtraKey("model")] = sharedTicketProgressRaw(292, sharedTicketProgressNow.Add(time.Minute))
			}
			snapshot := NewSharedPoolTicketAccountSnapshot(account, sharedTicketProgressNow)
			snapshot.Available, snapshot.Concurrency = tc.available, tc.concurrency
			capacity := &SharedPoolCapacity{TotalAccounts: 9, AvailableAccounts: 4, ConcurrencyCapacity: 8,
				ConcurrencyUnlimited: tc.unlimited > 0, UnlimitedAccounts: tc.unlimited, TicketAccounts: []SharedPoolTicketAccountSnapshot{*snapshot}}
			before := *capacity
			got := GetSharedPoolCatalogCapacity(capacity, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: []string{"model"}}, sharedTicketProgressNow)
			require.Equal(t, before, *capacity)
			require.EqualValues(t, 9, got.TotalAccounts)
			require.Equal(t, tc.wantAccounts, got.AvailableAccounts)
			require.Equal(t, tc.wantCap, got.ConcurrencyCapacity)
			require.Equal(t, tc.wantUnlimited, got.UnlimitedAccounts)
			require.Equal(t, tc.wantUnlimited > 0, got.ConcurrencyUnlimited)
		})
	}
}

func TestSharedPoolTicketSnapshotIsDetachedReadonlyAndSafeToSerialize(t *testing.T) {
	account := sharedTicketProgressAccount()
	raw := sharedTicketProgressRaw(292, sharedTicketProgressNow.Add(time.Hour))
	account.Extra[openAICodexTicketExtraKey("model")] = raw
	account.Extra["codex_harvest_proxy_url"] = "private-proxy-sentinel"
	before, err := json.Marshal(account)
	require.NoError(t, err)
	capacity := sharedTicketProgressCapacity(t, account)
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: []string{"model"}}
	progress := GetSharedPoolCatalogCapacity(capacity, cfg, sharedTicketProgressNow)
	after, err := json.Marshal(account)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after), "读取快照和计算进度不得修改来源账号")
	for _, value := range []any{capacity.TicketAccounts[0], capacity, progress} {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "TicketAccounts")
		require.NotContains(t, string(encoded), "UnlimitedAccounts")
		for _, secret := range []string{raw["state"].(string), "credential-sentinel", "private-proxy-sentinel", "access_token"} {
			require.NotContains(t, string(encoded), secret)
			require.NotContains(t, fmt.Sprintf("%#v", value), secret, "内部快照也不应持有票据正文或凭证")
		}
	}
	raw["state"], raw["expires_at"] = "changed", sharedTicketProgressNow.Add(-time.Hour)
	account.Credentials["access_token"] = "changed"
	require.Equal(t, progress, GetSharedPoolCatalogCapacity(capacity, cfg, sharedTicketProgressNow))
	require.EqualValues(t, 9, capacity.TotalAccounts, "进度不修改池内账号分母")
}
