//go:build unit

package repository

import (
	"context"
	"errors"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolInitialTierAssignmentUsesEffectiveState(t *testing.T) {
	for _, tc := range []struct {
		name, extra, credentials, patch string
		owner                           bool
		override                        *string
		failOutbox                      bool
	}{
		{name: "owner consent upgrade", owner: true, extra: `{"shared_pool_dispatch_consent":false}`,
			credentials: `{"plan_type":"prolite"}`, patch: `{"shared_pool_dispatch_consent":true}`},
		{name: "admin first enable", extra: `{"shared_pool_dispatch_consent":true}`,
			credentials: `{"plan_type":"prolite"}`, patch: `{}`},
		{name: "admin override and enable", extra: `{"shared_pool_dispatch_consent":true}`, override: new("prolite"),
			credentials: `{"plan_type":"pro"}`, patch: `{"shared_pool_subscription_tier":"prolite"}`},
		{name: "admin clear override and enable", extra: `{"shared_pool_dispatch_consent":true,"shared_pool_subscription_tier":"pro"}`, override: new(""),
			credentials: `{"plan_type":"prolite"}`, patch: `{"shared_pool_subscription_tier":null}`},
		{name: "outbox failure rolls back", owner: true, extra: `{"shared_pool_dispatch_consent":false}`, failOutbox: true,
			credentials: `{"plan_type":"prolite"}`, patch: `{"shared_pool_dispatch_consent":true}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newSharedDailyCooldownRepository(t)
			state := service.SharedPoolAccountState{Enabled: new(true), SubscriptionTier: tc.override}
			if tc.owner {
				state.OwnerID, state.DispatchConsent, state.GroupIDs = 7, new(true), &[]int64{11}
			} else {
				state.DefaultGroupIDs = []int64{11}
			}
			mock.ExpectBegin()
			mock.ExpectQuery("(?s)SELECT a.id FROM accounts a JOIN shared_pool_accounts.*FOR UPDATE OF a,s").WithArgs(int64(41), state.OwnerID).
				WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(41))
			mock.ExpectQuery("SELECT s.admin_disabled,s.assigned,a.extra").WithArgs(int64(41)).
				WillReturnRows(sqlmock.NewRows([]string{"admin_disabled", "assigned", "extra"}).AddRow(false, false, []byte(tc.extra)))
			if tc.override != nil {
				expectSharedInitialAccountKind(mock)
			}
			mock.ExpectQuery("SELECT group_id FROM account_groups").WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{"group_id"}))
			expectSharedInitialAccountKind(mock)
			mock.ExpectQuery("(?s)"+regexp.QuoteMeta("SELECT (is_shared_pool OR $4)")+".*FOR SHARE").WithArgs(int64(11), "openai", "oauth", true, false).
				WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(false))
			mock.ExpectQuery("SELECT platform,type,credentials,extra FROM accounts").WithArgs(int64(41)).
				WillReturnRows(sqlmock.NewRows([]string{"platform", "type", "credentials", "extra"}).AddRow("openai", "oauth", []byte(tc.credentials), []byte(tc.extra)))
			mock.ExpectQuery("SELECT id,platform,status,subscription_type,is_exclusive,is_shared_pool,require_oauth_only").WithArgs(int64(11)).
				WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "status", "subscription_type", "is_exclusive", "is_shared_pool", "require_oauth_only"}).
					AddRow(11, "openai", "active", "standard", true, false, true))
			mock.ExpectQuery("SELECT subscription_group_ids FROM shared_pool_settings").WillReturnRows(
				sqlmock.NewRows([]string{"subscription_group_ids"}).AddRow([]byte(`{"openai":{"prolite":[11]}}`)))
			mock.ExpectExec("DELETE FROM account_groups").WithArgs(int64(41), "{11}").WillReturnResult(sqlmock.NewResult(0, 0))
			mock.ExpectExec("INSERT INTO account_groups").WithArgs(int64(41), int64(11)).WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectExec("UPDATE shared_pool_accounts SET enabled=").WithArgs(int64(41), true, nil, true).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec("UPDATE accounts a SET extra=").WithArgs(int64(41), tc.patch).WillReturnResult(sqlmock.NewResult(0, 1))
			outbox := mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs(service.SchedulerOutboxEventAccountChanged, int64(41), nil, sqlmock.AnyArg(), sqlmock.AnyArg())
			if tc.failOutbox {
				outbox.WillReturnError(errors.New("fixture outbox unavailable"))
				mock.ExpectRollback()
			} else {
				outbox.WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			}
			err := repo.SetSharedAccountState(context.Background(), 41, state)
			if tc.failOutbox {
				require.ErrorContains(t, err, "fixture outbox unavailable")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func expectSharedInitialAccountKind(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT platform,type FROM accounts").WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"platform", "type"}).AddRow("openai", "oauth"))
}
