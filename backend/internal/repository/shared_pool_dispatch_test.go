//go:build unit

package repository

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedDispatchStateActorCannotForgeAuthorization(t *testing.T) {
	for _, state := range []service.SharedPoolAccountState{
		{DispatchConsent: new(true)},
		{OwnerID: 7, AdminDisabled: new(false)}, {OwnerID: -1},
		{OwnerID: 7, SubscriptionTier: new("pro")}, {OwnerID: 7, DefaultGroupID: new(int64(5))},
		{OwnerID: 7, Priority: new(1)}, {Priority: new(-1)}, {Priority: new(101)},
	} {
		repo, _ := newSharedDailyCooldownRepository(t)
		require.Error(t, repo.SetSharedAccountState(context.Background(), 41, state))
	}
}

func TestSharedDispatchStateChecksCommittedConsent(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra map[string]any
		state service.SharedPoolAccountState
		want  string
		fail  bool
	}{
		{name: "legacy unchanged", state: service.SharedPoolAccountState{OwnerID: 7, Enabled: new(true)}, want: `{}`},
		{name: "new account cannot enable without consent", extra: map[string]any{service.SharedPoolDispatchConsentKey: false}, state: service.SharedPoolAccountState{OwnerID: 7, Enabled: new(true)}, fail: true},
		{name: "explicit upgrade", state: service.SharedPoolAccountState{OwnerID: 7, Enabled: new(true), DispatchConsent: new(true)}, want: `{"shared_pool_dispatch_consent":true}`},
		{name: "repeat upgrade preserves other settings", extra: map[string]any{service.SharedPoolDispatchConsentKey: true}, state: service.SharedPoolAccountState{OwnerID: 7, Enabled: new(true), DispatchConsent: new(true)}, want: `{}`},
		{name: "disable preserves consent", extra: map[string]any{service.SharedPoolDispatchConsentKey: true}, state: service.SharedPoolAccountState{OwnerID: 7, Enabled: new(false)}, want: `{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, patch, err := prepareSharedDispatchState(tc.extra, &tc.state)
			if tc.fail {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.JSONEq(t, tc.want, patch)
		})
	}
}

func expectSharedDispatchStateLock(mock sqlmock.Sqlmock, owner int64, extra string) {
	mock.ExpectBegin()
	mock.ExpectQuery("(?s)SELECT a.id FROM accounts a JOIN shared_pool_accounts.*FOR UPDATE OF a,s").WithArgs(int64(41), owner).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(41))
	mock.ExpectQuery("SELECT s.admin_disabled,s.assigned,a.extra").WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"admin_disabled", "assigned", "extra"}).AddRow(false, true, []byte(extra)))
	mock.ExpectQuery("SELECT group_id FROM account_groups").WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{"group_id"}).AddRow(4))
}

func TestSharedDispatchStateUpgradeNeedsNoGroupAndCommitsOutboxAtomically(t *testing.T) {
	for _, failOutbox := range []bool{false, true} {
		repo, mock := newSharedDailyCooldownRepository(t)
		expectSharedDispatchStateLock(mock, 7, `{}`)
		mock.ExpectExec("UPDATE shared_pool_accounts SET enabled=").WithArgs(int64(41), true, nil, false).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec("UPDATE accounts a SET extra=").WithArgs(int64(41), `{"shared_pool_dispatch_consent":true}`).WillReturnResult(sqlmock.NewResult(0, 1))
		outbox := mock.ExpectExec("INSERT INTO scheduler_outbox").WithArgs(service.SchedulerOutboxEventAccountChanged, int64(41), nil, sqlmock.AnyArg(), sqlmock.AnyArg())
		if failOutbox {
			outbox.WillReturnError(errors.New("outbox unavailable"))
			mock.ExpectRollback()
		} else {
			outbox.WillReturnResult(sqlmock.NewResult(1, 1))
			mock.ExpectCommit()
		}
		err := repo.SetSharedAccountState(context.Background(), 41, service.SharedPoolAccountState{OwnerID: 7, Enabled: new(true), DispatchConsent: new(true)})
		if failOutbox {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestSharedDispatchAdminAssignmentRollsBackWithStateFailure(t *testing.T) {
	repo, mock := newSharedDailyCooldownRepository(t)
	expectSharedDispatchStateLock(mock, 0, `{"shared_pool_dispatch_consent":true}`)
	mock.ExpectQuery("SELECT platform,type FROM accounts").WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{"platform", "type"}).AddRow("openai", "oauth"))
	mock.ExpectQuery(`(?s)SELECT \(is_shared_pool OR \$4\).*FOR SHARE`).WithArgs(int64(9), "openai", "oauth", true, true).WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(true))
	mock.ExpectExec("DELETE FROM account_groups").WithArgs(int64(41), "{9}").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("INSERT INTO account_groups").WithArgs(int64(41), int64(9)).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec("UPDATE shared_pool_accounts SET enabled=").WithArgs(int64(41), nil, nil, true).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE accounts a SET extra=").WithArgs(int64(41), `{}`).WillReturnError(errors.New("state write failure"))
	mock.ExpectRollback()
	groups := []int64{9}
	require.Error(t, repo.SetSharedAccountState(context.Background(), 41, service.SharedPoolAccountState{GroupIDs: &groups}))
}

func TestSharedDispatchUserRateOverrideValidationAndReset(t *testing.T) {
	for _, value := range []float64{-1, 101, math.Inf(1), math.NaN()} {
		repo, _ := newSharedDailyCooldownRepository(t)
		require.Error(t, repo.SaveSharedUserRate(context.Background(), service.SharedPoolUserRate{UserID: 7, SettlementMultiplier: &value}))
	}
	for _, value := range []*float64{nil, new(0.0), new(2.5)} {
		repo, mock := newSharedDailyCooldownRepository(t)
		mock.ExpectBegin()
		mock.ExpectQuery(`(?s)SELECT COALESCE\(\$1,platform_rate_bps\).*FOR UPDATE`).WithArgs(nil, nil).WillReturnRows(sqlmock.NewRows([]string{"valid"}).AddRow(true))
		mock.ExpectExec("INSERT INTO shared_pool_user_rates").WithArgs(int64(7), nil, nil, value).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()
		require.NoError(t, repo.SaveSharedUserRate(context.Background(), service.SharedPoolUserRate{UserID: 7, SettlementMultiplier: value}))
	}
}
