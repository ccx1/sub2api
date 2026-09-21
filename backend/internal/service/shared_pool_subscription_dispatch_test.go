package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedPoolAdmissionSupportsAssignedSubscriptionAndExclusiveGroups(t *testing.T) {
	for _, kind := range []string{SubscriptionTypeStandard, SubscriptionTypeSubscription} {
		for _, exclusive := range []bool{false, true} {
			ctx, repo, resolve := sharedAdmissionFixture()
			repo.group.SubscriptionType, repo.group.IsExclusive = kind, exclusive
			latest, vetoed, reason := sharedPoolAdmissionLatest(ctx, repo.account, repo, resolve)
			require.False(t, vetoed, "kind=%s exclusive=%t: %s", kind, exclusive, reason)
			require.Equal(t, .5, latest.SharedPoolSettlement.Multiplier)
			require.Equal(t, 1.0, latest.SharedPoolSettlement.ConsumerTokenMultiplier)
			repo.terms.Multiplier = 1.1
			_, vetoed, reason = sharedPoolAdmissionLatest(ctx, repo.account, repo, resolve)
			require.True(t, vetoed)
			require.Equal(t, "shared_settlement_unfunded", reason)
		}
	}
}

func TestSharedPoolSubscriptionBillingUsesQuotaAndAccountSettlementSnapshot(t *testing.T) {
	_, repo, _ := sharedAdmissionFixture()
	repo.group.SubscriptionType = SubscriptionTypeSubscription
	repo.account.SharedPoolSettlement = repo.terms
	subscription := &UserSubscription{ID: 12}
	params := &postUsageBillingParams{
		Cost: &CostBreakdown{TotalCost: 10, ActualCost: 20}, User: &User{ID: 7},
		APIKey:  &APIKey{ID: 1, GroupID: &repo.group.ID, Group: repo.group},
		Account: repo.account, Subscription: subscription, IsSubscriptionBill: true,
	}
	cmd := buildUsageBillingCommand("shared-subscription", &UsageLog{TotalCost: 10}, params)
	require.Equal(t, subscription.ID, *cmd.SubscriptionID)
	require.Equal(t, 20.0, cmd.SubscriptionCost)
	require.Zero(t, cmd.BalanceCost)
	require.Equal(t, 10.0, cmd.SharedPoolBaseCost)
	require.Equal(t, .5, *cmd.SharedPoolSettlementMultiplier)
	require.Equal(t, 500, *cmd.SharedPoolPlatformRateBPS)
	require.False(t, cmd.SharedPoolGroup, "modern settlement does not depend on a legacy shared marker")
	repo.terms.Multiplier = 3
	require.Equal(t, .5, *cmd.SharedPoolSettlementMultiplier)
}

type sharedSubscriptionCatalogRepo struct {
	GroupRepository
	requested []int64
}

func (r *sharedSubscriptionCatalogRepo) SharedPoolAvailableCapacities(_ context.Context, ids []int64) (map[int64]SharedPoolCapacity, error) {
	r.requested = ids
	result := make(map[int64]SharedPoolCapacity)
	for _, id := range ids {
		result[id] = SharedPoolCapacity{AvailableAccounts: 1}
	}
	return result, nil
}

func TestSharedAccountCatalogIncludesAuthorizedSubscriptionAndExclusiveGroups(t *testing.T) {
	repo := &sharedSubscriptionCatalogRepo{}
	svc := &APIKeyService{groupRepo: repo}
	groups := []Group{
		{ID: 1, Status: StatusActive, Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeSubscription},
		{ID: 2, Status: StatusActive, Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeStandard, IsExclusive: true},
		{ID: 3, Status: StatusDisabled, Platform: PlatformOpenAI, SubscriptionType: SubscriptionTypeSubscription},
		{ID: 4, Status: StatusActive, Platform: PlatformOpenAI, SubscriptionType: "unsupported"},
	}
	got, err := svc.SharedAccountCatalogGroups(context.Background(), groups)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 2}, repo.requested)
	require.Len(t, got, 2)
	require.Equal(t, int64(1), got[0].ActiveAccountCount)
	require.Nil(t, groups[0].SharedPoolCapacity)
}
