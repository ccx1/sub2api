//go:build sharedpoolintegration

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	entschema "entgo.io/ent/dialect/sql/schema"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccountgroup "github.com/Wei-Shaw/sub2api/ent/accountgroup"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

type sharedAccountPG struct {
	db                 *sql.DB
	client             *dbent.Client
	repo               *sharedPoolRepository
	owner, other, a, b int64
}

func sharedAccountPostgresFixture(t *testing.T) sharedAccountPG {
	t.Helper()
	dsn := os.Getenv("SUB2API_SHARED_POOL_TEST_DSN")
	if dsn == "" {
		t.Skip("SUB2API_SHARED_POOL_TEST_DSN is not set")
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", parsed.Hostname())
	require.Equal(t, "/codex_shared_pool_test", parsed.Path)
	admin, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := fmt.Sprintf("shared_accounts_test_%d", time.Now().UnixNano())
	_, err = admin.Exec(`CREATE SCHEMA ` + pq.QuoteIdentifier(schema))
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	db, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() {
		client.Close()
		_, _ = admin.Exec(`DROP SCHEMA ` + pq.QuoteIdentifier(schema) + ` CASCADE`)
		admin.Close()
	})
	ctx := context.Background()
	require.NoError(t, client.Schema.Create(ctx, entschema.WithSchemaName(schema)))
	// Ent 的 time.Now 默认只在客户端生效；补齐生产 001_init.sql 的数据库默认值。
	_, err = db.Exec(`ALTER TABLE account_groups ALTER COLUMN created_at SET DEFAULT NOW()`)
	require.NoError(t, err)
	for _, name := range []string{"036_scheduler_outbox.sql", "152_scheduler_outbox_dedup_key.sql", "153_scheduler_outbox_pending_dedup_key_index_notx.sql", "250_shared_pool_accounts.sql", "251_shared_pool_earnings.sql", "252_shared_pool_subscription_groups.sql", "253_shared_pool_independent_settlement.sql", "255_shared_pool_settlement_tier_multipliers.sql", "256_shared_pool_default_priority.sql"} {
		body, e := os.ReadFile(filepath.Join("..", "..", "migrations", name))
		require.NoError(t, e)
		_, e = db.Exec(string(body))
		require.NoError(t, e, "migration %s", name)
	}
	owner, err := client.User.Create().SetEmail("owner@example.invalid").SetPasswordHash("fixture-only").Save(ctx)
	require.NoError(t, err)
	other, err := client.User.Create().SetEmail("other@example.invalid").SetPasswordHash("fixture-only").Save(ctx)
	require.NoError(t, err)
	a, err := client.Group.Create().SetName("Pool A").SetPlatform(service.PlatformOpenAI).SetIsSharedPool(true).Save(ctx)
	require.NoError(t, err)
	b, err := client.Group.Create().SetName("Pool B").SetPlatform(service.PlatformOpenAI).SetIsSharedPool(true).Save(ctx)
	require.NoError(t, err)
	repo := NewSharedPoolRepository(client, db, nil).(*sharedPoolRepository)
	return sharedAccountPG{db, client, repo, owner.ID, other.ID, a.ID, b.ID}
}

func (f sharedAccountPG) account(name string, groups ...int64) *service.Account {
	return &service.Account{Name: name, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "fixture-" + name}, Concurrency: 3, Priority: 50, Status: service.StatusActive, Schedulable: true,
		GroupIDs: groups, Extra: map[string]any{service.SharedPoolOwnerKey: f.owner, service.SharedPoolEnabledKey: len(groups) > 0, service.SharedPoolAdminDisabledKey: false}}
}

func TestSharedPoolAccountsPostgresCreateAtomicAndOwnership(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	a := f.account("first", f.a)
	require.NoError(t, f.repo.CreateSharedAccount(ctx, a, f.owner, "credential-one"))
	record, err := f.repo.GetSharedAccount(ctx, f.owner, a.ID)
	require.NoError(t, err)
	require.True(t, record.Enabled)
	require.True(t, record.Assigned)
	require.Equal(t, f.owner, record.OwnerUserID)
	links, err := f.client.AccountGroup.Query().Where(dbaccountgroup.AccountIDEQ(a.ID)).Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, links)
	_, err = f.repo.GetSharedAccount(ctx, f.other, a.ID)
	require.ErrorIs(t, err, service.ErrSharedPoolAccountNotFound)
	items, total, err := f.repo.ListSharedAccounts(ctx, f.other, 1, 20)
	require.NoError(t, err)
	require.Empty(t, items)
	require.Zero(t, total)
	duplicate := f.account("duplicate", f.b)
	require.Error(t, f.repo.CreateSharedAccount(ctx, duplicate, f.other, "credential-one"))
	count, err := f.client.Account.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	wrong := f.account("invalid", f.b)
	wrong.Platform = service.PlatformAnthropic
	require.Error(t, f.repo.CreateSharedAccount(ctx, wrong, f.owner, "credential-wrong-platform"))
	count, err = f.client.Account.Query().Count(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	var outbox int
	require.NoError(t, f.db.QueryRow(`SELECT COUNT(*) FROM scheduler_outbox`).Scan(&outbox))
	require.Equal(t, 1, outbox)
}

func TestSharedPoolAccountsPostgresAssignmentSwitchesAndAdminStop(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	a := f.account("state")
	require.NoError(t, f.repo.CreateSharedAccount(ctx, a, f.owner, "credential-state"))
	on, off := true, false
	groupA := []int64{f.a}
	both := []int64{f.a, f.b}
	empty := []int64{}
	require.Error(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{OwnerID: f.owner, Enabled: &on}))
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{OwnerID: f.owner, Enabled: &on, GroupIDs: &groupA}))
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{GroupIDs: &both}))
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{OwnerID: f.owner, Enabled: &off}))
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{OwnerID: f.owner, Enabled: &on}))
	ids, err := f.client.AccountGroup.Query().Where(dbaccountgroup.AccountIDEQ(a.ID)).Select(dbaccountgroup.FieldGroupID).Ints(ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, []int{int(f.a), int(f.b)}, ids)
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{AdminDisabled: &on}))
	require.Error(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{OwnerID: f.owner, Enabled: &on}))
	saved, err := f.client.Account.Get(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, true, saved.Extra[service.SharedPoolAdminDisabledKey])
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{AdminDisabled: &off, GroupIDs: &empty}))
	record, err := f.repo.GetSharedAccount(ctx, f.owner, a.ID)
	require.NoError(t, err)
	require.True(t, record.Assigned)
	require.Error(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{OwnerID: f.owner, Enabled: &on}))
	count, err := f.client.AccountGroup.Query().Where(dbaccountgroup.AccountIDEQ(a.ID)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	require.ErrorIs(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{OwnerID: f.other, Enabled: &off}), service.ErrSharedPoolAccountNotFound)
}

func TestSharedPoolAccountsPostgresDispatchConsentAndConcurrentAssignment(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	a := f.account("dispatch", f.a)
	require.NoError(t, f.repo.CreateSharedAccount(ctx, a, f.owner, "dispatch-credential"))
	ordinary, err := f.client.Group.Create().SetName("Ordinary").SetPlatform(service.PlatformOpenAI).Save(ctx)
	require.NoError(t, err)
	groups := []int64{ordinary.ID}
	require.Error(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{GroupIDs: &groups}))
	require.Error(t, f.repo.accounts.BindGroups(ctx, a.ID, groups))
	require.Error(t, f.repo.accounts.AddToGroup(ctx, a.ID, ordinary.ID, 50))
	require.Error(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{DispatchConsent: new(true)}))
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{OwnerID: f.owner, Enabled: new(true), DispatchConsent: new(true)}))
	var workers sync.WaitGroup
	errs := make(chan error, 2)
	workers.Add(2)
	go func() {
		defer workers.Done()
		errs <- f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{GroupIDs: &groups})
	}()
	go func() {
		defer workers.Done()
		errs <- f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{OwnerID: f.owner, Enabled: new(false)})
	}()
	workers.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	record, err := f.repo.GetSharedAccount(ctx, f.owner, a.ID)
	require.NoError(t, err)
	require.False(t, record.Enabled)
	saved, err := f.repo.accounts.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, groups, saved.GroupIDs)
	require.True(t, service.SharedPoolDispatchConsented(saved))
	require.False(t, saved.IsSchedulable())
	require.NoError(t, f.repo.accounts.BindGroups(ctx, a.ID, nil))
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{OwnerID: f.owner, Enabled: new(true)}))
}

func TestSharedPoolAccountsPostgresCredentialConflictRollsBack(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	a, b := f.account("one", f.a), f.account("two", f.b)
	b.Credentials["_token_version"] = int64(9999999999999)
	proxy, err := f.client.Proxy.Create().SetName("profile-proxy").SetProtocol("http").SetHost("profile.example.invalid").SetPort(8080).Save(ctx)
	require.NoError(t, err)
	b.ProxyID = &proxy.ID
	require.NoError(t, f.repo.CreateSharedAccount(ctx, a, f.owner, "identity-one"))
	require.NoError(t, f.repo.CreateSharedAccount(ctx, b, f.owner, "identity-two"))
	replacement := map[string]any{"access_token": "replacement-fixture"}
	input := service.SharedPoolAccountUpdate{Name: "updated", Concurrency: 5, Credentials: replacement, Fingerprint: "identity-one", ProxyChanged: true}
	require.Error(t, f.repo.UpdateSharedAccount(ctx, f.owner, b.ID, input))
	saved, err := f.client.Account.Get(ctx, b.ID)
	require.NoError(t, err)
	require.Equal(t, "two", saved.Name)
	require.Equal(t, 3, saved.Concurrency)
	require.Equal(t, &proxy.ID, saved.ProxyID)
	require.Equal(t, "fixture-two", saved.Credentials["access_token"])
	var fingerprint string
	require.NoError(t, f.db.QueryRow(`SELECT credential_fingerprint FROM shared_pool_accounts WHERE account_id=$1`, b.ID).Scan(&fingerprint))
	require.Equal(t, "identity-two", fingerprint)
	input.Fingerprint = "identity-new"
	require.ErrorIs(t, f.repo.UpdateSharedAccount(ctx, f.other, b.ID, input), service.ErrSharedPoolAccountNotFound)
	require.NoError(t, f.repo.UpdateSharedAccount(ctx, f.owner, b.ID, input))
	saved, err = f.client.Account.Get(ctx, b.ID)
	require.NoError(t, err)
	require.Equal(t, "updated", saved.Name)
	require.Equal(t, 5, saved.Concurrency)
	require.Nil(t, saved.ProxyID)
	require.Equal(t, "random", saved.Extra[service.ProxyModeExtraKey])
	require.Equal(t, "reject", saved.Extra[service.RandomProxyEmptyPoolPolicyExtraKey])
	require.Equal(t, "replacement-fixture", saved.Credentials["access_token"])
	require.Greater(t, saved.Credentials["_token_version"].(float64), float64(9999999999999))
	previousVersion := saved.Credentials["_token_version"].(float64)
	require.NoError(t, f.repo.UpdateSharedAccount(ctx, f.owner, b.ID, input))
	saved, err = f.client.Account.Get(ctx, b.ID)
	require.NoError(t, err)
	require.Greater(t, saved.Credentials["_token_version"].(float64), previousVersion)
}

func TestSharedPoolAccountsPostgresProfileProxyPreservesSwitches(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	a := f.account("profile", f.a)
	a.Extra[service.AntiDegradationExtraKey] = true
	proxy, err := f.client.Proxy.Create().SetName("custom-proxy").SetProtocol("http").SetHost("custom.example.invalid").SetPort(8080).Save(ctx)
	require.NoError(t, err)
	a.ProxyID = &proxy.ID
	require.NoError(t, f.repo.CreateSharedAccount(ctx, a, f.owner, "identity-profile"))
	input := service.SharedPoolAccountUpdate{Name: "random-profile", Concurrency: 2, ProxyChanged: true}
	require.NoError(t, f.repo.UpdateSharedAccount(ctx, f.owner, a.ID, input))
	paused, disabled := false, true
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{Enabled: &paused, AdminDisabled: &disabled}))
	input.Name, input.ProxyID = "custom-profile", &proxy.ID
	require.NoError(t, f.repo.UpdateSharedAccount(ctx, f.owner, a.ID, input))
	saved, err := f.client.Account.Get(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, "custom-profile", saved.Name)
	require.Equal(t, &proxy.ID, saved.ProxyID)
	require.NotContains(t, saved.Extra, service.ProxyModeExtraKey)
	require.NotContains(t, saved.Extra, service.RandomProxyEmptyPoolPolicyExtraKey)
	require.EqualValues(t, f.owner, saved.Extra[service.SharedPoolOwnerKey])
	require.Equal(t, false, saved.Extra[service.SharedPoolEnabledKey])
	require.Equal(t, true, saved.Extra[service.SharedPoolAdminDisabledKey])
	require.Equal(t, true, saved.Extra[service.AntiDegradationExtraKey])
	require.Equal(t, "fixture-profile", saved.Credentials["access_token"])
}

func TestSharedPoolAccountsPostgresPrivateProxyIsolation(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	public, err := f.client.Proxy.Create().SetName("public").SetProtocol("http").SetHost("proxy.example.invalid").SetPort(8080).Save(ctx)
	require.NoError(t, err)
	input := &service.Proxy{Name: "private", Protocol: "http", Host: "private.example.invalid", Port: 8081}
	private, err := f.repo.CreateSharedProxy(ctx, f.owner, input, "proxy-fingerprint")
	require.NoError(t, err)
	again, err := f.repo.CreateSharedProxy(ctx, f.owner, input, "proxy-fingerprint")
	require.NoError(t, err)
	require.Equal(t, private.ID, again.ID)
	selected, err := f.repo.accounts.SelectRandomActiveProxy(ctx)
	require.NoError(t, err)
	require.Equal(t, public.ID, selected.ID)
	selected, err = f.repo.accounts.SelectRandomActiveProxyFromPool(ctx, []int64{private.ID})
	require.NoError(t, err)
	require.Nil(t, selected)
	allocator := &ProxyPoolAllocator{client: f.client}
	candidates, err := allocator.readCandidates(ctx, service.ProxyPoolSelection{})
	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, public.ID, candidates[0].proxy.ID)
}

func TestSharedPoolAccountsPostgresRatesValidateAcrossOverrides(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	cfg, err := f.repo.SharedSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 2000, cfg.PlatformRateBPS)
	require.Equal(t, 100, cfg.ProxyRateBPS)
	high, zero := 9900, 0
	require.NoError(t, f.repo.SaveSharedUserRate(ctx, service.SharedPoolUserRate{UserID: f.owner, PlatformRateBPS: &high}))
	changed := *cfg
	changed.ProxyRateBPS = 101
	require.Error(t, f.repo.SaveSharedSettings(ctx, &changed))
	unchanged, err := f.repo.SharedSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, 100, unchanged.ProxyRateBPS)
	require.NoError(t, f.repo.SaveSharedUserRate(ctx, service.SharedPoolUserRate{UserID: f.owner, PlatformRateBPS: &high, ProxyRateBPS: &zero}))
	require.NoError(t, f.repo.SaveSharedSettings(ctx, &changed))
	require.Error(t, f.repo.SaveSharedUserRate(ctx, service.SharedPoolUserRate{UserID: f.owner, PlatformRateBPS: &high}))
	rates, err := f.repo.SharedUserRates(ctx)
	require.NoError(t, err)
	require.Len(t, rates, 1)
	require.NotNil(t, rates[0].ProxyRateBPS)
	require.Zero(t, *rates[0].ProxyRateBPS)
	require.NoError(t, f.repo.SaveSharedUserRate(ctx, service.SharedPoolUserRate{UserID: f.owner}))
	rates, err = f.repo.SharedUserRates(ctx)
	require.NoError(t, err)
	require.Nil(t, rates[0].PlatformRateBPS)
	require.Nil(t, rates[0].ProxyRateBPS)
}
