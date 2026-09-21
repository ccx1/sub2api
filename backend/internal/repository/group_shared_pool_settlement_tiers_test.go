package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func sharedCapacitySettlementRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"account_id", "owner_user_id", "settlement_multiplier", "subscription_settlement_multipliers", "user_multiplier", "platform_rate_bps", "proxy_rate_bps"})
}

func TestSharedPoolCapacitySettlementMultiplierPriority(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
	repo := newGroupRepositoryWithSQL(nil, db)
	var accounts []*service.Account
	for id, tier := range []string{"free", "pro", "pro", "team", "plus", "free", "free"} {
		accounts = append(accounts, &service.Account{ID: int64(id + 1), Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
			Credentials: map[string]any{"plan_type": tier}, Extra: map[string]any{service.SharedPoolDispatchConsentKey: true}})
	}
	accounts[6].Extra[service.SharedPoolSubscriptionTierKey] = "pro"
	accounts = append(accounts, accounts[0], nil, &service.Account{ID: 99}, &service.Account{ID: 100, Extra: map[string]any{service.SharedPoolDispatchConsentKey: false}})
	tiers := `{"openai":{"free":0.25,"pro":2,"plus":0}}`
	rows := sharedCapacitySettlementRows().
		AddRow(1, 7, 1, tiers, nil, 500, 100).
		AddRow(2, 7, 1, tiers, nil, 500, 100).
		AddRow(3, 8, 1, tiers, 3.0, 250, 50).
		AddRow(4, 7, 1, tiers, nil, 500, 100).
		AddRow(5, 7, 1, tiers, nil, 500, 100).
		AddRow(6, 8, 1, tiers, 0.0, 250, 50).
		AddRow(7, 7, 1, tiers, nil, 500, 100)
	mock.ExpectQuery("SELECT spa.account_id,spa.owner_user_id,s.settlement_multiplier").WithArgs("{1,2,3,4,5,6,7}").WillReturnRows(rows)
	terms, err := repo.sharedPoolSettlementByAccounts(context.Background(), accounts)
	require.NoError(t, err)
	require.Len(t, terms, 7)
	for id, expected := range map[int64]float64{1: .25, 2: 2, 3: 3, 4: 1, 5: 0, 6: 0, 7: 2} {
		require.Equal(t, expected, terms[id].Multiplier, "account %d", id)
	}
	require.Equal(t, 250, terms[3].PlatformRateBPS)
	require.Equal(t, 50, terms[3].ProxyRateBPS)
}

func TestSharedPoolCapacityUsesTierSettlementForFunding(t *testing.T) {
	for _, tc := range []struct {
		name      string
		plan      string
		user      any
		available int64
	}{
		{name: "pro tier exceeds group revenue", plan: "pro"},
		{name: "user override restores funding", plan: "pro", user: .25, available: 1},
		{name: "unconfigured tier uses global", plan: "team", available: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, client := newAPIKeyRepoSQLite(t)
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
			ctx, repo := context.Background(), newGroupRepositoryWithSQL(client, db)
			group, err := client.Group.Create().SetName(tc.name).SetPlatform(service.PlatformOpenAI).SetRateMultiplier(.5).Save(ctx)
			require.NoError(t, err)
			account, err := client.Account.Create().SetName("supply").SetPlatform(service.PlatformOpenAI).SetType(service.AccountTypeOAuth).
				SetCredentials(map[string]any{"plan_type": tc.plan}).SetConcurrency(3).SetSchedulable(true).
				SetExtra(map[string]any{service.SharedPoolOwnerKey: 7, service.SharedPoolEnabledKey: true, service.SharedPoolDispatchConsentKey: true}).Save(ctx)
			require.NoError(t, err)
			mock.ExpectQuery("SELECT ag.account_id, ag.group_id").WithArgs(sqlmock.AnyArg(), service.StatusActive).
				WillReturnRows(sqlmock.NewRows([]string{"account_id", "group_id"}).AddRow(account.ID, group.ID))
			mock.ExpectQuery("SELECT spa.account_id,spa.owner_user_id,s.settlement_multiplier").WithArgs(sqlmock.AnyArg()).
				WillReturnRows(sharedCapacitySettlementRows().AddRow(account.ID, 7, .1, `{"openai":{"pro":2}}`, tc.user, 500, 100))
			capacities, err := repo.SharedPoolAvailableCapacities(ctx, []int64{group.ID})
			require.NoError(t, err)
			require.Equal(t, tc.available, capacities[group.ID].AvailableAccounts)
			require.Equal(t, tc.available*3, capacities[group.ID].ConcurrencyCapacity)
		})
	}
}

func TestSharedPoolCapacitySettlementInvalidConfigFailsClosed(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close(); require.NoError(t, mock.ExpectationsWereMet()) })
	repo := newGroupRepositoryWithSQL(nil, db)
	accounts := []*service.Account{{ID: 1, Extra: map[string]any{service.SharedPoolDispatchConsentKey: true}}}
	mock.ExpectQuery("SELECT spa.account_id,spa.owner_user_id,s.settlement_multiplier").WithArgs("{1}").
		WillReturnRows(sharedCapacitySettlementRows().AddRow(1, 7, 1, `{`, nil, 500, 100))
	terms, err := repo.sharedPoolSettlementByAccounts(context.Background(), accounts)
	require.Error(t, err)
	require.Nil(t, terms)
}
