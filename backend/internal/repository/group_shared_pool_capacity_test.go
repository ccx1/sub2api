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

func TestSharedPoolCapacityUsesOnlyAvailableMembersAndCountsEachGroup(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo, ctx := newGroupRepositoryWithSQL(client, db), context.Background()
	first, err := client.Group.Create().SetName("pool-a").SetPlatform(service.PlatformOpenAI).SetIsSharedPool(true).Save(ctx)
	require.NoError(t, err)
	second, err := client.Group.Create().SetName("pool-b").SetPlatform(service.PlatformOpenAI).SetIsSharedPool(true).Save(ctx)
	require.NoError(t, err)
	members := sqlmock.NewRows([]string{"account_id", "group_id"})
	var availableIDs []int64
	for _, tc := range []struct {
		name        string
		concurrency int
		unavailable string
	}{
		{"both groups", 3, ""}, {"second account", 5, ""}, {"limited", 100, "limited"},
		{"disabled", 100, "disabled"}, {"cooldown", 100, "cooldown"}, {"proxy unavailable", 100, "proxy"},
		{"not shared", 100, "unbound"}, {"different platform", 100, "platform"},
	} {
		builder := client.Account.Create().SetName(tc.name).SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeOAuth).
			SetCredentials(map[string]any{}).SetSchedulable(true).SetConcurrency(tc.concurrency)
		switch tc.unavailable {
		case "limited":
			builder.SetRateLimitResetAt(time.Now().Add(time.Hour))
		case "disabled":
			builder.SetStatus(service.StatusDisabled)
		case "cooldown":
			builder.SetTempUnschedulableUntil(time.Now().Add(time.Hour))
		case "proxy":
			builder.SetExtra(map[string]any{service.ProxyModeExtraKey: service.ProxyModeRandom})
		case "platform":
			builder.SetPlatform(service.PlatformGemini)
		}
		account, err := builder.Save(ctx)
		require.NoError(t, err)
		if tc.unavailable == "" {
			availableIDs = append(availableIDs, account.ID)
		}
		if tc.unavailable != "unbound" {
			members.AddRow(account.ID, first.ID)
		}
		if tc.name == "both groups" {
			members.AddRow(account.ID, second.ID)
		}
	}
	mock.ExpectQuery("SELECT ag.account_id, ag.group_id").WithArgs(sqlmock.AnyArg(), service.StatusActive).WillReturnRows(members)
	capacities, err := repo.SharedPoolAvailableCapacities(ctx, []int64{first.ID, second.ID})
	require.NoError(t, err)
	require.EqualValues(t, 7, capacities[first.ID].TotalAccounts)
	require.EqualValues(t, 2, capacities[first.ID].AvailableAccounts)
	require.EqualValues(t, 8, capacities[first.ID].ConcurrencyCapacity)
	require.False(t, capacities[first.ID].ConcurrencyUnlimited)
	require.ElementsMatch(t, availableIDs, capacities[first.ID].AvailableAccountIDs)
	require.Empty(t, capacities[first.ID].UntrackedConcurrencyAccountIDs)
	require.Len(t, capacities[first.ID].TicketAccounts, 5, "打票等待统计包含限流、冷却、代理暂不可用账号，排除停用账号及其他平台")
	require.EqualValues(t, 1, capacities[second.ID].TotalAccounts)
	require.EqualValues(t, 1, capacities[second.ID].AvailableAccounts)
	require.EqualValues(t, 3, capacities[second.ID].ConcurrencyCapacity)
	require.Len(t, capacities[second.ID].TicketAccounts, 1)
	require.Equal(t, availableIDs[:1], capacities[second.ID].AvailableAccountIDs)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolCapacityHandlesUnlimitedAccountLimits(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo, ctx := newGroupRepositoryWithSQL(client, db), context.Background()
	group, err := client.Group.Create().SetName("pool").SetPlatform(service.PlatformOpenAI).SetIsSharedPool(true).Save(ctx)
	require.NoError(t, err)
	account, err := client.Account.Create().SetName("unlimited").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeOAuth).
		SetCredentials(map[string]any{}).SetConcurrency(0).SetSchedulable(true).Save(ctx)
	require.NoError(t, err)
	mock.ExpectQuery("SELECT ag.account_id, ag.group_id").WithArgs(sqlmock.AnyArg(), service.StatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "group_id"}).AddRow(account.ID, group.ID))
	capacities, err := repo.SharedPoolAvailableCapacities(ctx, []int64{group.ID})
	require.NoError(t, err)
	require.EqualValues(t, 1, capacities[group.ID].TotalAccounts)
	require.EqualValues(t, 1, capacities[group.ID].AvailableAccounts)
	require.True(t, capacities[group.ID].ConcurrencyUnlimited)
	require.Zero(t, capacities[group.ID].ConcurrencyCapacity)
	require.Equal(t, []int64{account.ID}, capacities[group.ID].AvailableAccountIDs)
	require.Equal(t, []int64{account.ID}, capacities[group.ID].UntrackedConcurrencyAccountIDs)
	empty, err := repo.SharedPoolAvailableCapacities(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, empty)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolCapacityUsesProtectionLimitForUnboundedAccounts(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo, ctx := newGroupRepositoryWithSQL(client, db), context.Background()
	group, err := client.Group.Create().SetName("protected-pool").SetPlatform(service.PlatformOpenAI).SetIsSharedPool(true).Save(ctx)
	require.NoError(t, err)
	members := sqlmock.NewRows([]string{"account_id", "group_id"})
	for _, concurrency := range []int{0, 3} {
		account, err := client.Account.Create().SetName("protected").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeOAuth).
			SetCredentials(map[string]any{}).SetConcurrency(concurrency).SetSchedulable(true).
			SetExtra(map[string]any{service.AntiDegradationExtraKey: true}).Save(ctx)
		require.NoError(t, err)
		members.AddRow(account.ID, group.ID)
	}
	mock.ExpectQuery("SELECT ag.account_id, ag.group_id").WithArgs(sqlmock.AnyArg(), service.StatusActive).WillReturnRows(members)
	capacities, err := repo.SharedPoolAvailableCapacities(ctx, []int64{group.ID})
	require.NoError(t, err)
	require.EqualValues(t, 2, capacities[group.ID].TotalAccounts)
	require.EqualValues(t, 2, capacities[group.ID].AvailableAccounts)
	require.EqualValues(t, service.AntiDegradeConcurrencyCap+3, capacities[group.ID].ConcurrencyCapacity)
	require.False(t, capacities[group.ID].ConcurrencyUnlimited)
	require.Len(t, capacities[group.ID].UntrackedConcurrencyAccountIDs, 1)
	require.Contains(t, capacities[group.ID].AvailableAccountIDs, capacities[group.ID].UntrackedConcurrencyAccountIDs[0])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestSharedPoolCapacityRequiresFundedModernSupplyAndPreservesLegacyScope(t *testing.T) {
	for _, tc := range []struct {
		name              string
		shared            bool
		consent           *bool
		price, settlement float64
		missingTerms      bool
		available         int64
	}{
		{name: "ordinary funded", consent: new(true), price: 2, settlement: 1, available: 1},
		{name: "ordinary underfunded", consent: new(true), price: .5, settlement: 1},
		{name: "free group with free supply", consent: new(true)},
		{name: "shared modern underfunded", shared: true, consent: new(true), price: .5, settlement: 1},
		{name: "legacy shared keeps old rule", shared: true, available: 1},
		{name: "legacy ordinary stays blocked", price: 2},
		{name: "explicit nonconsent shared stays blocked", shared: true, consent: new(false), price: 2},
		{name: "missing owner terms", consent: new(true), price: 2, missingTerms: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, client := newAPIKeyRepoSQLite(t)
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
			repo, ctx := newGroupRepositoryWithSQL(client, db), context.Background()
			group, err := client.Group.Create().SetName(tc.name).SetPlatform(service.PlatformOpenAI).SetIsSharedPool(tc.shared).SetRateMultiplier(tc.price).Save(ctx)
			require.NoError(t, err)
			extra := map[string]any{service.SharedPoolOwnerKey: 7, service.SharedPoolEnabledKey: true}
			if tc.consent != nil {
				extra[service.SharedPoolDispatchConsentKey] = *tc.consent
			}
			account, err := client.Account.Create().SetName("supply").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeOAuth).
				SetCredentials(map[string]any{}).SetConcurrency(3).SetSchedulable(true).SetExtra(extra).Save(ctx)
			require.NoError(t, err)
			mock.ExpectQuery("SELECT ag.account_id, ag.group_id").WithArgs(sqlmock.AnyArg(), service.StatusActive).
				WillReturnRows(sqlmock.NewRows([]string{"account_id", "group_id"}).AddRow(account.ID, group.ID))
			if tc.consent != nil && *tc.consent {
				rows := sqlmock.NewRows([]string{"account_id", "owner_user_id", "settlement_multiplier", "subscription_settlement_multipliers", "user_multiplier", "platform_rate_bps", "proxy_rate_bps"})
				if !tc.missingTerms {
					rows.AddRow(account.ID, 7, tc.settlement, `{}`, nil, 500, 100)
				}
				mock.ExpectQuery(`SELECT spa.account_id,spa.owner_user_id,s.settlement_multiplier`).WithArgs(sqlmock.AnyArg()).WillReturnRows(rows)
			}
			capacities, err := repo.SharedPoolAvailableCapacities(ctx, []int64{group.ID})
			require.NoError(t, err)
			require.EqualValues(t, 1, capacities[group.ID].TotalAccounts)
			require.Equal(t, tc.available, capacities[group.ID].AvailableAccounts)
			require.Equal(t, tc.available*3, capacities[group.ID].ConcurrencyCapacity)
		})
	}
}

func TestSharedPoolCapacitySettlementQueryFailureDoesNotAdvertiseSupply(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
	repo, ctx := newGroupRepositoryWithSQL(client, db), context.Background()
	account, err := client.Account.Create().SetName("supply").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeOAuth).
		SetCredentials(map[string]any{}).SetExtra(map[string]any{service.SharedPoolDispatchConsentKey: true}).Save(ctx)
	require.NoError(t, err)
	mock.ExpectQuery("SELECT ag.account_id, ag.group_id").WithArgs(sqlmock.AnyArg(), service.StatusActive).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "group_id"}).AddRow(account.ID, 1))
	failure := errors.New("settlement configuration unavailable")
	mock.ExpectQuery("SELECT spa.account_id,spa.owner_user_id,s.settlement_multiplier").WithArgs(sqlmock.AnyArg()).WillReturnError(failure)
	capacities, err := repo.SharedPoolAvailableCapacities(ctx, []int64{1})
	require.ErrorIs(t, err, failure)
	require.Empty(t, capacities)
}
