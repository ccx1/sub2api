//go:build sharedpoolintegration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedAdminStatePostgresTierEnablePriorityAndReset(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	a := f.account("admin-state", f.a)
	a.Extra[service.SharedPoolDispatchConsentKey] = true
	a.Extra[service.SharedPoolEnabledKey] = false
	a.Extra[service.OpenAICodexTicketEnabledExtraKey] = false
	a.Extra[service.AntiDegradationExtraKey] = true
	a.Extra[service.DailyCooldownExtraKey] = map[string]any{"enabled": false}
	a.Credentials["plan_type"] = "free"
	a.Status, a.ErrorMessage, a.Schedulable = "error", "preserve upstream error", false
	require.NoError(t, f.repo.CreateSharedAccount(ctx, a, f.owner, "admin-state"))
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{AdminDisabled: new(true)}))
	groups := []int64{f.a, f.b}
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{Enabled: new(true), AdminDisabled: new(false), SubscriptionTier: new("prolite"), Priority: new(17), GroupIDs: &groups}))
	stored, err := f.client.Account.Get(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, "prolite", stored.Extra[service.SharedPoolSubscriptionTierKey])
	require.Equal(t, false, stored.Extra[service.OpenAICodexTicketEnabledExtraKey], "修改共享档位不能开启打票")
	require.Equal(t, true, stored.Extra[service.AntiDegradationExtraKey])
	require.Equal(t, map[string]any{"enabled": false}, stored.Extra[service.DailyCooldownExtraKey])
	require.Equal(t, "error", stored.Status)
	require.Equal(t, new("preserve upstream error"), stored.ErrorMessage)
	require.False(t, stored.Schedulable)
	require.Equal(t, 17, stored.Priority)
	var groupPriority int
	require.NoError(t, f.db.QueryRow(`SELECT MIN(priority) FROM account_groups WHERE account_id=$1`, a.ID).Scan(&groupPriority))
	require.Equal(t, 17, groupPriority)
	record, err := f.repo.GetSharedAccount(ctx, 0, a.ID)
	require.NoError(t, err)
	require.True(t, record.Enabled)
	require.False(t, record.AdminDisabled)
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{SubscriptionTier: new("")}))
	stored, err = f.client.Account.Get(ctx, a.ID)
	require.NoError(t, err)
	require.NotContains(t, stored.Extra, service.SharedPoolSubscriptionTierKey)
	require.Equal(t, "free", service.SharedPoolOverviewTierForAccount(accountEntityToService(stored)))
	_, err = f.db.Exec(`UPDATE account_groups SET priority=29 WHERE account_id=$1 AND group_id=$2`, a.ID, f.a)
	require.NoError(t, err)
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{GroupIDs: &groups}))
	require.NoError(t, f.db.QueryRow(`SELECT priority FROM account_groups WHERE account_id=$1 AND group_id=$2`, a.ID, f.a).Scan(&groupPriority))
	require.Equal(t, 29, groupPriority, "retained assignments preserve their priority unless priority is explicitly updated")
}

func TestSharedAdminStatePostgresFailureRollsBackTierPriorityAndDispatch(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	a := f.account("state-rollback", f.a)
	a.Extra[service.SharedPoolDispatchConsentKey] = true
	a.Extra[service.SharedPoolEnabledKey] = false
	a.Extra[service.OpenAICodexTicketEnabledExtraKey] = false
	a.Credentials["plan_type"] = "free"
	require.NoError(t, f.repo.CreateSharedAccount(ctx, a, f.owner, "state-rollback"))
	_, err := f.db.Exec(`ALTER TABLE scheduler_outbox ADD CONSTRAINT reject_test_outbox CHECK(false) NOT VALID`)
	require.NoError(t, err)
	groups := []int64{f.b}
	require.Error(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{Enabled: new(true), Priority: new(13), SubscriptionTier: new("pro"), GroupIDs: &groups}))
	stored, err := f.client.Account.Get(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, a.Priority, stored.Priority)
	require.Equal(t, false, stored.Extra[service.OpenAICodexTicketEnabledExtraKey])
	require.NotContains(t, stored.Extra, service.SharedPoolSubscriptionTierKey)
	record, err := f.repo.GetSharedAccount(ctx, 0, a.ID)
	require.NoError(t, err)
	require.False(t, record.Enabled)
	var groupID int64
	require.NoError(t, f.db.QueryRow(`SELECT group_id FROM account_groups WHERE account_id=$1`, a.ID).Scan(&groupID))
	require.Equal(t, f.a, groupID)
}

func TestSharedAdminStatePostgresConsentCannotBeForgedAndTierSurvivesRefresh(t *testing.T) {
	f := sharedAccountPostgresFixture(t)
	ctx := context.Background()
	a := f.account("state-consent", f.a)
	a.Extra[service.SharedPoolDispatchConsentKey] = false
	a.Extra[service.SharedPoolEnabledKey] = false
	a.Credentials["plan_type"] = "free"
	require.NoError(t, f.repo.CreateSharedAccount(ctx, a, f.owner, "state-consent"))
	require.Error(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{Enabled: new(true)}))
	require.Error(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{OwnerID: f.owner, SubscriptionTier: new("pro")}))
	require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{SubscriptionTier: new("pro")}))
	stale := *a
	stale.Extra = map[string]any{"old_refresh": true}
	require.NoError(t, f.repo.accounts.Update(ctx, &stale))
	stored, err := f.client.Account.Get(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, "pro", stored.Extra[service.SharedPoolSubscriptionTierKey])
	require.Equal(t, false, stored.Extra[service.SharedPoolDispatchConsentKey])
	require.True(t, service.OpenAICodexTicketAccountEnabled(accountEntityToService(stored)))
}

func TestSharedAdminStatePostgresNilAndEmptyGroupsClearAssignments(t *testing.T) {
	for _, groups := range [][]int64{nil, {}} {
		f := sharedAccountPostgresFixture(t)
		ctx := context.Background()
		a := f.account("clear-groups", f.a)
		a.Extra[service.SharedPoolDispatchConsentKey] = true
		require.NoError(t, f.repo.CreateSharedAccount(ctx, a, f.owner, "clear-groups"))
		require.NoError(t, f.repo.SetSharedAccountState(ctx, a.ID, service.SharedPoolAccountState{GroupIDs: &groups}))
		var count int
		require.NoError(t, f.db.QueryRow(`SELECT COUNT(*) FROM account_groups WHERE account_id=$1`, a.ID).Scan(&count))
		require.Zero(t, count)
	}
}
