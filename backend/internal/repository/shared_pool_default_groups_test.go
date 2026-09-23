//go:build unit

package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedDefaultGroupsSettingsReadLegacyAndMultiple(t *testing.T) {
	for _, raw := range []string{`{"openai":4}`, `{"openai":[4,5]}`} {
		repo, mock := newSharedDailyCooldownRepository(t)
		mock.ExpectQuery("SELECT platform_rate_bps,proxy_rate_bps,max_concurrency,default_group_ids").
			WillReturnRows(sqlmock.NewRows([]string{"platform_rate_bps", "proxy_rate_bps", "max_concurrency", "defaults", "subscriptions", "multiplier", "tier_multipliers", "priority"}).
				AddRow(500, 100, 10, []byte(raw), []byte(`{}`), 1, []byte(`{}`), 17))
		settings, err := repo.SharedSettings(context.Background())
		require.NoError(t, err)
		want := []int64{4}
		if raw == `{"openai":[4,5]}` {
			want = append(want, 5)
		}
		require.Equal(t, want, settings.DefaultGroupIDs[service.PlatformOpenAI])
	}
}

func TestSharedDefaultGroupsSettingsSaveAllOrRollback(t *testing.T) {
	for _, invalidSecond := range []bool{false, true} {
		repo, mock := newSharedDailyCooldownRepository(t)
		settings := &service.SharedPoolSettings{MaxConcurrency: 10, SettlementMultiplier: 1,
			DefaultGroupIDs:      service.SharedPoolDefaultGroupIDs{service.PlatformOpenAI: {4, 5, 4}},
			SubscriptionGroupIDs: service.SharedPoolSubscriptionGroupIDs{}}
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT subscription_group_ids,subscription_settlement_multipliers FROM shared_pool_settings").
			WillReturnRows(sqlmock.NewRows([]string{"subscriptions", "multipliers"}).AddRow([]byte(`{}`), []byte(`{}`)))
		for _, id := range []int64{4, 5} {
			mock.ExpectQuery("SELECT status='active' AND platform=").WithArgs(id, service.PlatformOpenAI).
				WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(id != 5 || !invalidSecond))
		}
		if invalidSecond {
			mock.ExpectRollback()
		} else {
			mock.ExpectQuery("SELECT EXISTS.*shared_pool_user_rates").WithArgs(0, 0).WillReturnRows(sqlmock.NewRows([]string{"invalid"}).AddRow(false))
			mock.ExpectExec("UPDATE shared_pool_settings SET settlement_multiplier=").WithArgs(float64(1)).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("UPDATE shared_pool_settings SET platform_rate_bps=").
				WithArgs(0, 0, 10, `{"openai":[4,5]}`, `{}`, float64(1), `{}`, 0).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()
		}
		err := repo.SaveSharedSettings(context.Background(), settings)
		if invalidSecond {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
			require.Equal(t, []int64{4, 5}, settings.DefaultGroupIDs[service.PlatformOpenAI])
		}
	}
}

func TestSharedDefaultGroupsAssignAllOrRollback(t *testing.T) {
	for _, failSecond := range []bool{false, true} {
		repo, mock := newSharedDailyCooldownRepository(t)
		expectSharedAdminStateLock(mock, `{"shared_pool_dispatch_consent":true}`, false, false)
		mock.ExpectQuery("SELECT group_id FROM account_groups").WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{"group_id"}))
		mock.ExpectQuery("SELECT platform,type FROM accounts").WithArgs(int64(41)).
			WillReturnRows(sqlmock.NewRows([]string{"platform", "type"}).AddRow(service.PlatformOpenAI, service.AccountTypeOAuth))
		for _, id := range []int64{4, 5} {
			mock.ExpectQuery("SELECT .*is_shared_pool OR").WithArgs(id, service.PlatformOpenAI, service.AccountTypeOAuth, true, false).
				WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(true))
		}
		mock.ExpectExec("DELETE FROM account_groups").WithArgs(int64(41), "{4,5}").WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectExec("INSERT INTO account_groups").WithArgs(int64(41), int64(4)).WillReturnResult(sqlmock.NewResult(1, 1))
		second := mock.ExpectExec("INSERT INTO account_groups").WithArgs(int64(41), int64(5))
		if failSecond {
			second.WillReturnError(errors.New("second group insert failed"))
			mock.ExpectRollback()
		} else {
			second.WillReturnResult(sqlmock.NewResult(2, 1))
			mock.ExpectExec("UPDATE shared_pool_accounts SET enabled=").WithArgs(int64(41), true, nil, true).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("UPDATE accounts a SET extra=").WithArgs(int64(41), `{}`).WillReturnResult(sqlmock.NewResult(0, 1))
			expectSharedAdminStateOutbox(mock).WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()
		}
		err := repo.SetSharedAccountState(context.Background(), 41, service.SharedPoolAccountState{Enabled: new(true), DefaultGroupIDs: []int64{4, 5}})
		if failSecond {
			require.ErrorContains(t, err, "second group insert failed")
		} else {
			require.NoError(t, err)
		}
	}
}
