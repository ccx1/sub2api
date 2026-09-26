package repository

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

const sharedOverviewRegistrationQuery = `SELECT spa.account_id,spa.enabled,spa.admin_disabled,ag.group_id,COALESCE(u.status=$1,false)
	FROM shared_pool_accounts spa JOIN accounts a ON a.id=spa.account_id AND a.deleted_at IS NULL
	LEFT JOIN users u ON u.id=spa.owner_user_id AND u.deleted_at IS NULL
	LEFT JOIN account_groups ag ON ag.account_id=spa.account_id`

func TestSharedPoolOverviewRegistrationsRetainInactiveAndDeletedOwners(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
	rows := sqlmock.NewRows([]string{"account_id", "enabled", "admin_disabled", "group_id", "owner_active"}).
		AddRow(1, true, false, 7, true).
		AddRow(2, true, false, 7, false).
		AddRow(3, true, false, 7, false).
		AddRow(3, true, false, 8, false)
	mock.ExpectQuery(sharedOverviewRegistrationQuery).WithArgs(service.StatusActive).WillReturnRows(rows)
	repo := newGroupRepositoryWithSQL(nil, db)
	registrations, groupIDs, err := repo.sharedOverviewRegistrations(context.Background())
	require.NoError(t, err)
	require.Len(t, registrations, 3, "用户停用或删除时，共享账号仍计入总数")
	require.ElementsMatch(t, []int64{7, 8}, groupIDs)
	accounts := []*service.Account{}
	for id := int64(1); id <= 3; id++ {
		accounts = append(accounts, &service.Account{ID: id, Platform: service.PlatformOpenAI,
			Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true})
	}
	state := sharedOverviewState{registrations: registrations, groups: map[int64]*service.Group{
		7: {ID: 7, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, IsSharedPool: true},
	}}
	got := state.snapshots(accounts)
	require.Len(t, got, 3)
	for i, account := range got {
		require.True(t, account.Valid)
		require.Equal(t, i == 0, account.Available, "停用或已删除用户的账号不得调度")
	}
}

func TestSharedPoolOverviewRegistrationFailuresPropagate(t *testing.T) {
	for _, mode := range []string{"query", "scan", "rows"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
			query := mock.ExpectQuery(sharedOverviewRegistrationQuery).WithArgs(service.StatusActive)
			failure := errors.New("registration read failed")
			rows := sqlmock.NewRows([]string{"account_id", "enabled", "admin_disabled", "group_id", "owner_active"})
			switch mode {
			case "query":
				query.WillReturnError(failure)
			case "scan":
				query.WillReturnRows(rows.AddRow(1, true, false, 7, "invalid boolean"))
			case "rows":
				query.WillReturnRows(rows.AddRow(1, true, false, 7, true).RowError(0, failure))
			}
			got, err := newGroupRepositoryWithSQL(nil, db).SharedPoolOverviewAccounts(context.Background())
			require.Error(t, err)
			require.Empty(t, got)
			if mode != "scan" {
				require.ErrorIs(t, err, failure)
			}
		})
	}
}

func TestSharedPoolOverviewValidityUsesAccountStatusAndExpiration(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name      string
		status    string
		expiresAt *time.Time
		valid     bool
	}{
		{"active", service.StatusActive, nil, true},
		{"future", service.StatusActive, new(now.Add(time.Hour)), true},
		{"expired", service.StatusActive, new(now.Add(-time.Hour)), false},
		{"expiry boundary", service.StatusActive, &now, false},
		{"disabled", service.StatusDisabled, nil, false},
		{"error", service.StatusError, nil, false},
	} {
		for _, autoPause := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/without auto pause", true: "/with auto pause"}[autoPause], func(t *testing.T) {
				account := &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
					Status: tc.status, ExpiresAt: tc.expiresAt, AutoPauseOnExpired: autoPause,
					Credentials: map[string]any{"expires_at": now.Add(-time.Hour).Format(time.RFC3339)}}
				got := (sharedOverviewState{}).snapshots([]*service.Account{account})
				require.Equal(t, tc.valid, got[0].Valid, "OAuth token 过期或自动暂停设置不能改变账号有效期口径")
			})
		}
	}
}

func TestSharedPoolOverviewTicketRequirementUsesAccountKind(t *testing.T) {
	for _, tc := range []struct {
		platform, kind string
		parent         *int64
		required       bool
	}{
		{service.PlatformOpenAI, service.AccountTypeOAuth, nil, true},
		{service.PlatformOpenAI, service.AccountTypeSetupToken, nil, true},
		{service.PlatformOpenAI, service.AccountTypeAPIKey, nil, false},
		{service.PlatformOpenAI, service.AccountTypeOAuth, new(int64(9)), false},
		{service.PlatformAnthropic, service.AccountTypeOAuth, nil, false},
		{service.PlatformGemini, service.AccountTypeOAuth, nil, false},
	} {
		account := &service.Account{ID: 1, Platform: tc.platform, Type: tc.kind, ParentAccountID: tc.parent, Status: service.StatusActive}
		got := (sharedOverviewState{}).snapshots([]*service.Account{account})
		require.Equal(t, tc.required, got[0].TicketRequired, "%s/%s parent=%v", tc.platform, tc.kind, tc.parent)
	}
}

func TestSharedPoolOverviewSourceRetainsDisabledAndUnassignedAccounts(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
	repo, ctx := newGroupRepositoryWithSQL(client, db), context.Background()
	first, err := client.Group.Create().SetName("first").SetPlatform(service.PlatformOpenAI).SetIsSharedPool(true).Save(ctx)
	require.NoError(t, err)
	second, err := client.Group.Create().SetName("second").SetPlatform(service.PlatformOpenAI).SetIsSharedPool(true).Save(ctx)
	require.NoError(t, err)
	rows := sqlmock.NewRows([]string{"account_id", "enabled", "admin_disabled", "group_id", "owner_active"})
	var availableID int64
	for _, name := range []string{"available", "disabled", "admin disabled", "unassigned", "deleted", "proxy unhealthy"} {
		builder := client.Account.Create().SetName(name).SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeOAuth).
			SetCredentials(map[string]any{"plan_type": "plus", "access_token": "private-token"}).SetConcurrency(3).SetSchedulable(true)
		if name == "deleted" {
			builder.SetDeletedAt(time.Now())
		}
		if name == "proxy unhealthy" {
			builder.SetExtra(map[string]any{service.ProxyModeExtraKey: service.ProxyModeRandom})
		}
		account, err := builder.Save(ctx)
		require.NoError(t, err)
		if name == "unassigned" {
			rows.AddRow(account.ID, true, false, nil, true)
			continue
		}
		rows.AddRow(account.ID, name != "disabled", name == "admin disabled", first.ID, true)
		if name == "available" {
			availableID = account.ID
			rows.AddRow(account.ID, true, false, second.ID, true)
		}
	}
	pool, _ := newProxyPoolAllocatorTest(t, 1, poolCandidate(7))
	repo.proxyPool = pool
	require.NoError(t, pool.latencyCache.SetProxyLatency(ctx, 7, &service.ProxyLatencyInfo{Success: false, UpdatedAt: time.Now()}))
	mock.ExpectQuery(sharedOverviewRegistrationQuery).WithArgs(service.StatusActive).WillReturnRows(rows)
	got, err := repo.SharedPoolOverviewAccounts(ctx)
	require.NoError(t, err)
	require.Len(t, got, 5, "跨组只计一次，停用和待分配保留，已删除账号剔除")
	var available int
	for _, account := range got {
		require.Equal(t, "plus", account.Tier)
		if account.Available {
			available++
			require.Equal(t, availableID, account.AccountID)
		}
	}
	require.Equal(t, 1, available)
	require.Zero(t, pool.rdb.ZCard(ctx, proxyPoolLeaseKey("7")).Val(), "浏览概览不能占用随机代理租约")
}

func TestSharedPoolOverviewTicketRequirementHonorsAccountSwitch(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    any
		required bool
	}{
		{"default enabled", nil, true},
		{"explicit enabled", true, true},
		{"explicit disabled", false, false},
		{"legacy string retains default", "false", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Status: service.StatusActive, Credentials: map[string]any{"plan_type": "plus"},
				Extra: map[string]any{service.OpenAICodexTicketEnabledExtraKey: tc.value}}
			got := (sharedOverviewState{}).snapshots([]*service.Account{account})
			require.Len(t, got, 1)
			require.Equal(t, tc.required, got[0].TicketRequired)
			require.Equal(t, tc.required, got[0].Ticket != nil)
		})
	}
}

func TestSharedPoolOverviewSnapshotsRespectActualSchedulingRules(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(*service.Account, *service.Group, *sharedOverviewRegistration, *service.SharedPoolSettlementTerms)
		available bool
	}{
		{"modern ordinary", func(*service.Account, *service.Group, *sharedOverviewRegistration, *service.SharedPoolSettlementTerms) {
		}, true},
		{"zero settlement remains valid", func(_ *service.Account, _ *service.Group, _ *sharedOverviewRegistration, terms *service.SharedPoolSettlementTerms) {
			terms.Multiplier = 0
		}, true},
		{"underfunded group", func(_ *service.Account, g *service.Group, _ *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			g.RateMultiplier = .5
		}, false},
		{"disabled group", func(_ *service.Account, g *service.Group, _ *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			g.Status = service.StatusDisabled
		}, false},
		{"incompatible platform", func(_ *service.Account, g *service.Group, _ *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			g.Platform = service.PlatformGemini
		}, false},
		{"legacy ordinary", func(a *service.Account, _ *service.Group, _ *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			delete(a.Extra, service.SharedPoolDispatchConsentKey)
		}, false},
		{"legacy shared", func(a *service.Account, g *service.Group, _ *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			delete(a.Extra, service.SharedPoolDispatchConsentKey)
			g.IsSharedPool = true
		}, true},
		{"explicit nonconsent shared", func(a *service.Account, g *service.Group, _ *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			a.Extra[service.SharedPoolDispatchConsentKey] = false
			g.IsSharedPool = true
		}, false},
		{"registration disabled", func(_ *service.Account, _ *service.Group, r *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			r.enabled = false
		}, false},
		{"registration admin disabled", func(_ *service.Account, _ *service.Group, r *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			r.adminDisabled = true
		}, false},
		{"owner disabled or deleted", func(_ *service.Account, _ *service.Group, r *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			r.ownerActive = false
		}, false},
		{"unassigned", func(_ *service.Account, _ *service.Group, r *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			r.groups = nil
		}, false},
		{"disabled account", func(a *service.Account, _ *service.Group, _ *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			a.Status = service.StatusDisabled
		}, false},
		{"scheduling disabled", func(a *service.Account, _ *service.Group, _ *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			a.Schedulable = false
		}, false},
		{"rate limited", func(a *service.Account, _ *service.Group, _ *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			a.RateLimitResetAt = new(time.Now().Add(time.Hour))
		}, false},
		{"privacy required", func(_ *service.Account, g *service.Group, _ *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			g.RequirePrivacySet = true
		}, false},
		{"random empty", func(a *service.Account, _ *service.Group, _ *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			a.Extra[service.ProxyModeExtraKey] = service.ProxyModeRandom
		}, false},
		{"fixed proxy missing", func(a *service.Account, _ *service.Group, _ *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			a.ProxyID = new(int64(8))
		}, false},
		{"daily cooldown", func(a *service.Account, _ *service.Group, _ *sharedOverviewRegistration, _ *service.SharedPoolSettlementTerms) {
			a.Extra[service.DailyCooldownExtraKey] = map[string]any{"enabled": true, "start": time.Now().UTC().Add(-time.Hour).Format("15:04"), "end": time.Now().UTC().Add(time.Hour).Format("15:04"), "timezone": "UTC"}
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &service.Account{ID: 9, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Concurrency: 3,
				Extra: map[string]any{service.SharedPoolOwnerKey: 7, service.SharedPoolEnabledKey: true, service.SharedPoolDispatchConsentKey: true}}
			group := &service.Group{ID: 1, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 2}
			registration := &sharedOverviewRegistration{enabled: true, ownerActive: true, groups: []int64{1}}
			terms := &service.SharedPoolSettlementTerms{Multiplier: 1, PlatformRateBPS: 500, ProxyRateBPS: 100}
			tc.change(account, group, registration, terms)
			state := sharedOverviewState{registrations: map[int64]*sharedOverviewRegistration{9: registration},
				groups: map[int64]*service.Group{1: group}, terms: map[int64]*service.SharedPoolSettlementTerms{9: terms}, proxies: &sharedPoolProxyAvailability{}}
			for _, ticketEnabled := range []bool{true, false} {
				account.Extra[service.OpenAICodexTicketEnabledExtraKey] = ticketEnabled
				got := state.snapshots([]*service.Account{account})
				require.Len(t, got, 1, "不可用账号也应保留总数")
				require.Equal(t, tc.available, got[0].Available, "ticket enabled=%v", ticketEnabled)
				require.Equal(t, ticketEnabled, got[0].TicketRequired)
				require.Equal(t, account.Status == service.StatusActive, got[0].Valid,
					"有效账号独立于共享开关、用户状态和实时调度准入条件")
			}
		})
	}
}

func TestSharedPoolOverviewAnyCompatibleGroupAndProtectionCapacity(t *testing.T) {
	account := &service.Account{ID: 9, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, Concurrency: 0,
		Extra: map[string]any{service.AntiDegradationExtraKey: true, service.SharedPoolDispatchConsentKey: true}}
	groups := map[int64]*service.Group{
		1: {ID: 1, Platform: service.PlatformOpenAI, Status: service.StatusDisabled, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 2},
		2: {ID: 2, Platform: service.PlatformOpenAI, Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard, RateMultiplier: 2},
	}
	registrations := map[int64]*sharedOverviewRegistration{9: {enabled: true, ownerActive: true, groups: []int64{1, 2}}}
	terms := map[int64]*service.SharedPoolSettlementTerms{9: {Multiplier: 1}}
	state := sharedOverviewState{registrations: registrations, groups: groups, terms: terms, proxies: &sharedPoolProxyAvailability{}}
	got := state.snapshots([]*service.Account{account})
	require.True(t, got[0].Available)
	require.Equal(t, service.AntiDegradeConcurrencyCap, got[0].Concurrency)
	require.True(t, got[0].UntrackedConcurrency)
	state.terms = nil
	got = state.snapshots([]*service.Account{account})
	require.False(t, got[0].Available, "现代授权账号缺少有效结算条款时关闭调度")
}

func TestSharedPoolOverviewSourceBatchesOwnerTermsAndPropagatesFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "zero owner multiplier", true: "query failed"}[fail], func(t *testing.T) {
			_, client := newAPIKeyRepoSQLite(t)
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
			repo := NewSharedPoolRepository(client, db, nil).(*sharedPoolRepository)
			ctx := context.Background()
			group, err := client.Group.Create().SetName("ordinary").SetPlatform(service.PlatformOpenAI).SetRateMultiplier(2).Save(ctx)
			require.NoError(t, err)
			members := sqlmock.NewRows([]string{"account_id", "enabled", "admin_disabled", "group_id", "owner_active"})
			terms := sqlmock.NewRows([]string{"account_id", "owner_user_id", "settlement_multiplier", "subscription_settlement_multipliers", "user_multiplier", "platform_rate_bps", "proxy_rate_bps"})
			for i := 0; i < 2; i++ {
				account, err := client.Account.Create().SetName("modern").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeOAuth).
					SetCredentials(map[string]any{}).SetSchedulable(true).SetConcurrency(3).SetExtra(map[string]any{service.SharedPoolDispatchConsentKey: true}).Save(ctx)
				require.NoError(t, err)
				members.AddRow(account.ID, true, false, group.ID, true)
				terms.AddRow(account.ID, 7, 1, `{}`, 0, 500, 100)
			}
			mock.ExpectQuery("SELECT spa.account_id,spa.enabled,spa.admin_disabled,ag.group_id").WithArgs(service.StatusActive).WillReturnRows(members)
			query := mock.ExpectQuery("SELECT spa.account_id,spa.owner_user_id,s.settlement_multiplier").WithArgs(sqlmock.AnyArg())
			failure := errors.New("terms unavailable")
			if fail {
				query.WillReturnError(failure)
			} else {
				query.WillReturnRows(terms)
			}
			got, err := repo.SharedPoolOverviewAccounts(ctx)
			if fail {
				require.ErrorIs(t, err, failure)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Len(t, got, 2)
			for _, account := range got {
				require.True(t, account.Available)
			}
		})
	}
}
