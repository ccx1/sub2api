//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

type sharedDailyCooldownRepo struct {
	sharedPoolRepoStub
	accounts *sharedTicketAccounts
	creates  int
	proxies  int
	last     SharedPoolAccountUpdate
}

func (r *sharedDailyCooldownRepo) CreateSharedAccount(_ context.Context, a *Account, owner int64, _ string) error {
	r.creates++
	a.ID = 1
	r.accounts.account = a
	r.record = SharedPoolAccountRecord{AccountID: a.ID, OwnerUserID: owner}
	return nil
}

func (r *sharedDailyCooldownRepo) UpdateSharedAccount(_ context.Context, _, _ int64, in SharedPoolAccountUpdate) error {
	r.updateCalls++
	r.last = in
	r.accounts.account.Name, r.accounts.account.Concurrency = in.Name, in.Concurrency
	if in.DailyCooldown != nil {
		r.accounts.account.Extra[DailyCooldownExtraKey] = in.DailyCooldown.ExtraValue()
	}
	return nil
}

func (r *sharedDailyCooldownRepo) CreateSharedProxy(_ context.Context, _ int64, proxy *Proxy, _ string) (*Proxy, error) {
	r.proxies++
	proxy.ID = 9
	return proxy, nil
}

func sharedDailyCooldownFixture() (*SharedPoolService, *sharedDailyCooldownRepo, SharedPoolAccountInput) {
	accounts := &sharedTicketAccounts{}
	repo := &sharedDailyCooldownRepo{accounts: accounts}
	svc := &SharedPoolService{repo: repo, accounts: accounts, earnings: sharedPoolTotalsStub{}}
	in := SharedPoolAccountInput{Name: "daily", Platform: PlatformGemini, Type: AccountTypeAPIKey, Concurrency: 3,
		Credentials: map[string]any{"api_key": "fixture-only"}}
	return svc, repo, in
}

func TestSharedPoolDailyCooldownCreateAndUpdateRoundTrip(t *testing.T) {
	svc, repo, in := sharedDailyCooldownFixture()
	in.DailyCooldown = &SharedPoolDailyCooldown{Enabled: true, Start: "23:00", End: "08:00"}
	view, err := svc.Create(context.Background(), 7, in)
	require.NoError(t, err)
	require.Equal(t, &SharedPoolDailyCooldown{Enabled: true, Start: "23:00", End: "08:00", Timezone: "Asia/Shanghai"}, view.DailyCooldown)
	require.Empty(t, in.DailyCooldown.Timezone, "规范化不得修改调用方输入")
	require.Equal(t, map[string]any{"enabled": true, "start": "23:00", "end": "08:00", "timezone": "Asia/Shanghai"}, repo.accounts.account.Extra[DailyCooldownExtraKey])
	for _, tc := range []struct {
		at string
		in bool
	}{{"2026-09-20T22:59:00+08:00", false}, {"2026-09-20T23:00:00+08:00", true}, {"2026-09-21T07:59:00+08:00", true}, {"2026-09-21T08:00:00+08:00", false}} {
		at, err := time.Parse(time.RFC3339, tc.at)
		require.NoError(t, err)
		require.Equal(t, tc.in, repo.accounts.account.IsInDailyCooldown(at))
	}
	in.Credentials = nil
	in.DailyCooldown = &SharedPoolDailyCooldown{Enabled: true, Start: "09:30", End: "12:00", Timezone: "America/New_York"}
	view, err = svc.Update(context.Background(), 7, 1, in)
	require.NoError(t, err)
	require.Equal(t, in.DailyCooldown, view.DailyCooldown)
	require.NotSame(t, in.DailyCooldown, repo.last.DailyCooldown)
	require.Equal(t, "random", repo.accounts.account.Extra[ProxyModeExtraKey])
	require.Equal(t, int64(7), repo.accounts.account.Extra[SharedPoolOwnerKey])
	require.Equal(t, false, repo.accounts.account.Extra[AntiDegradationExtraKey])
}

func TestSharedPoolDailyCooldownMissingNullAndExplicitDisable(t *testing.T) {
	svc, repo, in := sharedDailyCooldownFixture()
	view, err := svc.Create(context.Background(), 7, in)
	require.NoError(t, err)
	require.Nil(t, view.DailyCooldown)
	require.NotContains(t, repo.accounts.account.Extra, DailyCooldownExtraKey)
	repo.accounts.account.Extra[DailyCooldownExtraKey] = dailyCooldownExtra("23:00", "08:00", "UTC")[DailyCooldownExtraKey]
	for _, raw := range []string{`{}`, `{"daily_cooldown":null}`} {
		update := in
		update.Credentials = nil
		require.NoError(t, json.Unmarshal([]byte(raw), &update))
		view, err = svc.Update(context.Background(), 7, 1, update)
		require.NoError(t, err)
		require.Nil(t, repo.last.DailyCooldown)
		require.Equal(t, &SharedPoolDailyCooldown{Enabled: true, Start: "23:00", End: "08:00", Timezone: "UTC"}, view.DailyCooldown)
	}
	in.Credentials = nil
	in.DailyCooldown = &SharedPoolDailyCooldown{Start: "invalid", End: "invalid", Timezone: "Local"}
	view, err = svc.Update(context.Background(), 7, 1, in)
	require.NoError(t, err)
	require.Equal(t, &SharedPoolDailyCooldown{}, view.DailyCooldown)
	require.Equal(t, map[string]any{"enabled": false}, repo.accounts.account.Extra[DailyCooldownExtraKey])
	require.False(t, repo.accounts.account.IsInDailyCooldown(time.Now()))
	encoded, err := json.Marshal(view.DailyCooldown)
	require.NoError(t, err)
	require.JSONEq(t, `{"enabled":false}`, string(encoded))
}

func TestSharedPoolDailyCooldownSurvivesDefaultProtectionCreation(t *testing.T) {
	svc, repo, in := sharedDailyCooldownFixture()
	svc.protection = NewAntiDegradeService(nil, &protectionSettingsRepoStub{value: string(AntiDegradeModeMinimal)})
	in.Platform, in.Type, in.ProtectionEnabled = PlatformOpenAI, AccountTypeOAuth, true
	in.Credentials = map[string]any{"access_token": "fixture-only"}
	in.DailyCooldown = &SharedPoolDailyCooldown{Enabled: true, Start: "23:00", End: "08:00", Timezone: "UTC"}
	view, err := svc.Create(context.Background(), 7, in)
	require.NoError(t, err)
	require.True(t, view.ProtectionEnabled)
	require.Equal(t, "minimal_compat", repo.accounts.account.ProtectionMode())
	require.Equal(t, in.DailyCooldown, view.DailyCooldown)
	require.Equal(t, in.DailyCooldown.ExtraValue(), repo.accounts.account.Extra[DailyCooldownExtraKey])
}

func TestSharedPoolDailyCooldownRejectsInvalidBeforeProxyOrPersistence(t *testing.T) {
	for _, cooldown := range []SharedPoolDailyCooldown{
		{Enabled: true, Start: "24:00", End: "08:00"}, {Enabled: true, Start: "23:60", End: "08:00"},
		{Enabled: true, Start: "9:00", End: "08:00"}, {Enabled: true, Start: "23:00", End: "23:00"},
		{Enabled: true, End: "08:00"}, {Enabled: true, Start: "23:00", End: "08:00", Timezone: "Local"},
		{Enabled: true, Start: "23:00", End: "08:00", Timezone: "Mars/Unknown"},
	} {
		t.Run(cooldown.Start+cooldown.End+cooldown.Timezone, func(t *testing.T) {
			svc, repo, in := sharedDailyCooldownFixture()
			proxy := "http://8.8.8.8:8080"
			in.ProxyURL, in.DailyCooldown = &proxy, &cooldown
			_, err := svc.Create(context.Background(), 7, in)
			require.Equal(t, "INVALID_DAILY_COOLDOWN", infraerrors.Reason(err))
			require.Zero(t, repo.proxies)
			require.Zero(t, repo.creates)
			repo.record = SharedPoolAccountRecord{AccountID: 1, OwnerUserID: 7}
			repo.accounts.account = &Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeAPIKey}
			_, err = svc.Update(context.Background(), 7, 1, in)
			require.Equal(t, "INVALID_DAILY_COOLDOWN", infraerrors.Reason(err))
			require.Zero(t, repo.proxies)
			require.Zero(t, repo.updateCalls)
		})
	}
}

func TestSharedPoolDailyCooldownOwnershipStillPrecedesAccountRead(t *testing.T) {
	svc, repo, in := sharedDailyCooldownFixture()
	repo.record = SharedPoolAccountRecord{AccountID: 1, OwnerUserID: 7}
	in.DailyCooldown = &SharedPoolDailyCooldown{Enabled: true, Start: "23:00", End: "08:00"}
	_, err := svc.Update(context.Background(), 8, 1, in)
	require.ErrorIs(t, err, ErrSharedPoolAccountNotFound)
	require.Zero(t, repo.accounts.reads)
	require.Zero(t, repo.proxies)
	require.Zero(t, repo.updateCalls)
}

func TestSharedPoolDailyCooldownViewOnlyExposesSafeFields(t *testing.T) {
	extra := dailyCooldownExtra("23:00", "08:00", "UTC")
	extra[DailyCooldownExtraKey].(map[string]any)["private"] = "nested-secret"
	extra["token"] = "extra-secret"
	account := &Account{Extra: extra, Credentials: map[string]any{"access_token": "credential-secret"}}
	view := sharedAccountView(account, SharedPoolAccountRecord{}, 0, 0, false)
	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	for _, secret := range []string{"nested-secret", "extra-secret", "credential-secret", "credentials"} {
		require.NotContains(t, string(encoded), secret)
	}
	require.Equal(t, &SharedPoolDailyCooldown{Enabled: true, Start: "23:00", End: "08:00", Timezone: "UTC"}, view.DailyCooldown)
	view.DailyCooldown.Start = "09:00"
	require.Equal(t, "23:00", extra[DailyCooldownExtraKey].(map[string]any)["start"])
	for _, raw := range []any{nil, "invalid", map[string]any{"enabled": "true"}, map[string]any{"enabled": true, "start": "invalid"}} {
		require.Nil(t, sharedDailyCooldownView(map[string]any{DailyCooldownExtraKey: raw}))
	}
}
