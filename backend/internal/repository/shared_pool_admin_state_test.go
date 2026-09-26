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

func expectSharedAdminStateLock(mock sqlmock.Sqlmock, extra string, assigned, disabled bool) {
	mock.ExpectBegin()
	mock.ExpectQuery("(?s)SELECT a.id FROM accounts a JOIN shared_pool_accounts.*FOR UPDATE OF a,s").WithArgs(int64(41), int64(0)).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(41))
	mock.ExpectQuery("SELECT s.admin_disabled,s.assigned,a.extra").WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"admin_disabled", "assigned", "extra"}).AddRow(disabled, assigned, []byte(extra)))
}

func expectSharedAdminStateOutbox(mock sqlmock.Sqlmock) *sqlmock.ExpectedExec {
	return mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs(service.SchedulerOutboxEventAccountChanged, int64(41), nil, sqlmock.AnyArg(), sqlmock.AnyArg())
}

func TestSharedAdminStateTierAndEnableCommitOrRollbackTogether(t *testing.T) {
	for _, tc := range []struct {
		name, tier, patch string
		fail              bool
	}{
		{"set tier", "pro", `{"shared_pool_subscription_tier":"pro"}`, false},
		{"clear override", "", `{"shared_pool_subscription_tier":null}`, false},
		{"outbox failure", "pro", `{"shared_pool_subscription_tier":"pro"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newSharedDailyCooldownRepository(t)
			expectSharedAdminStateLock(mock, `{"shared_pool_dispatch_consent":true,"anti_degradation_enabled":true,"daily_cooldown":{"enabled":true}}`, true, true)
			mock.ExpectQuery("SELECT platform,type FROM accounts").WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{"platform", "type"}).AddRow("openai", "oauth"))
			mock.ExpectQuery("SELECT group_id FROM account_groups").WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{"group_id"}).AddRow(4))
			mock.ExpectExec("UPDATE shared_pool_accounts SET enabled=").WithArgs(int64(41), true, false, false).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("(?s)UPDATE accounts a SET extra=.*COALESCE.*shared_pool_subscription_tier.*ELSE '' END").WithArgs(int64(41), tc.patch).WillReturnResult(sqlmock.NewResult(0, 1))
			outbox := expectSharedAdminStateOutbox(mock)
			if tc.fail {
				outbox.WillReturnError(errors.New("outbox unavailable"))
				mock.ExpectRollback()
			} else {
				outbox.WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			}
			err := repo.SetSharedAccountState(context.Background(), 41, service.SharedPoolAccountState{Enabled: new(true), AdminDisabled: new(false), SubscriptionTier: &tc.tier})
			if tc.fail {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestSharedAdminStateRejectsUnconsentedEnableAndInvalidTier(t *testing.T) {
	for _, tier := range []*string{nil, new("pro")} {
		repo, mock := newSharedDailyCooldownRepository(t)
		expectSharedAdminStateLock(mock, `{"shared_pool_dispatch_consent":false}`, true, false)
		mock.ExpectRollback()
		require.Error(t, repo.SetSharedAccountState(context.Background(), 41, service.SharedPoolAccountState{Enabled: new(true), SubscriptionTier: tier}))
	}
	repo, mock := newSharedDailyCooldownRepository(t)
	expectSharedAdminStateLock(mock, `{"shared_pool_dispatch_consent":true}`, true, false)
	mock.ExpectQuery("SELECT platform,type FROM accounts").WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{"platform", "type"}).AddRow("openai", "oauth"))
	mock.ExpectRollback()
	require.Error(t, repo.SetSharedAccountState(context.Background(), 41, service.SharedPoolAccountState{SubscriptionTier: new("invalid-tier")}))
}

func TestSharedAdminStateDefaultCannotOverwriteConcurrentAssignment(t *testing.T) {
	for _, assigned := range []bool{false, true} {
		repo, mock := newSharedDailyCooldownRepository(t)
		expectSharedAdminStateLock(mock, `{"shared_pool_dispatch_consent":true}`, assigned, false)
		rows := sqlmock.NewRows([]string{"group_id"})
		if !assigned {
			rows.AddRow(4)
		}
		mock.ExpectQuery("SELECT group_id FROM account_groups").WithArgs(int64(41)).WillReturnRows(rows)
		mock.ExpectExec("UPDATE shared_pool_accounts SET enabled=").WithArgs(int64(41), true, nil, false).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("UPDATE accounts a SET extra=").WithArgs(int64(41), `{}`).WillReturnResult(sqlmock.NewResult(0, 1))
		expectSharedAdminStateOutbox(mock).WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		require.NoError(t, repo.SetSharedAccountState(context.Background(), 41, service.SharedPoolAccountState{Enabled: new(true), DefaultGroupIDs: []int64{9, 10}}))
	}
}

func TestSharedAdminStateLegacyEnableRemainsWithinSharedBindings(t *testing.T) {
	for _, count := range []int{0, 1} {
		repo, mock := newSharedDailyCooldownRepository(t)
		expectSharedAdminStateLock(mock, `{}`, true, false)
		mock.ExpectQuery("SELECT group_id FROM account_groups").WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{"group_id"}).AddRow(4))
		mock.ExpectQuery("(?s)SELECT COUNT.*g.is_shared_pool").WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
		if count == 0 {
			mock.ExpectRollback()
		} else {
			mock.ExpectExec("UPDATE shared_pool_accounts SET enabled=").WithArgs(int64(41), true, nil, false).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("UPDATE accounts a SET extra=").WithArgs(int64(41), `{}`).WillReturnResult(sqlmock.NewResult(0, 1))
			expectSharedAdminStateOutbox(mock).WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()
		}
		err := repo.SetSharedAccountState(context.Background(), 41, service.SharedPoolAccountState{Enabled: new(true), DefaultGroupIDs: []int64{9, 10}})
		if count == 0 {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestSharedProProfileDoesNotWriteTicketSwitch(t *testing.T) {
	for _, fail := range []bool{false, true} {
		repo, mock := newSharedDailyCooldownRepository(t)
		input := service.SharedPoolAccountUpdate{Name: "pro", Concurrency: 3}
		expectSharedDailyCooldownLock(mock).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(41))
		expectSharedDailyCooldownProfile(mock, input)
		outbox := expectSharedDailyCooldownOutbox(mock)
		if fail {
			outbox.WillReturnError(errors.New("outbox failure"))
			mock.ExpectRollback()
		} else {
			outbox.WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()
		}
		err := repo.UpdateSharedAccount(context.Background(), 7, 41, input)
		if fail {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
}
