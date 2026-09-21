package service

import (
	"context"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type sharedAdmissionRepository struct {
	AccountRepository
	account    *Account
	terms      *SharedPoolSettlementTerms
	err        error
	group      *Group
	groupErr   error
	groupReads int
}

func (r *sharedAdmissionRepository) GetByID(context.Context, int64) (*Account, error) {
	return r.account, r.err
}
func (r *sharedAdmissionRepository) SharedPoolSettlementTerms(context.Context, int64) (*SharedPoolSettlementTerms, error) {
	return r.terms, r.err
}

func (r *sharedAdmissionRepository) SharedPoolDispatchGroup(_ context.Context, _ int64) (*Group, error) {
	r.groupReads++
	return r.group, r.groupErr
}

func sharedAdmissionFixture() (context.Context, *sharedAdmissionRepository, sharedRateResolver) {
	g := &Group{ID: 3, Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 2}
	ctx := context.WithValue(context.Background(), ctxkey.Group, g)
	ctx = context.WithValue(ctx, ctxkey.UserID, int64(7))
	a := &Account{ID: 5, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, GroupIDs: []int64{3}, Extra: map[string]any{SharedPoolOwnerKey: int64(9), SharedPoolEnabledKey: true, SharedPoolDispatchConsentKey: true}}
	r := &sharedAdmissionRepository{account: a, group: g, terms: &SharedPoolSettlementTerms{Multiplier: .5, PlatformRateBPS: 500, ProxyRateBPS: 100}}
	return ctx, r, func(context.Context, int64, int64, float64) float64 { return 1 }
}

func TestSharedPoolAdmissionFreezesUserTermsWithoutMutatingRepository(t *testing.T) {
	ctx, repo, resolve := sharedAdmissionFixture()
	first, vetoed, _ := sharedPoolAdmissionLatest(ctx, repo.account, repo, resolve)
	require.False(t, vetoed)
	require.Nil(t, repo.account.SharedPoolSettlement)
	require.Equal(t, 1.0, first.SharedPoolSettlement.ConsumerTokenMultiplier)
	repo.terms.Multiplier = .8
	repo.terms.PlatformRateBPS = 1000
	second, vetoed, _ := sharedPoolAdmissionLatest(ctx, repo.account, repo, resolve)
	require.False(t, vetoed)
	require.Equal(t, .5, first.SharedPoolSettlement.Multiplier)
	require.Equal(t, 500, first.SharedPoolSettlement.PlatformRateBPS)
	require.Equal(t, .8, second.SharedPoolSettlement.Multiplier)
	other := *repo.account
	other.ID = 6
	repo.account = &other
	third, vetoed, _ := sharedPoolAdmissionLatest(ctx, &other, repo, resolve)
	require.False(t, vetoed)
	require.Equal(t, second.SharedPoolSettlement.Multiplier, third.SharedPoolSettlement.Multiplier)
}

func TestSharedPoolAdmissionRejectsStaleAuthorizationAndUnfundedPrices(t *testing.T) {
	for _, scenario := range []string{"paused", "admin disabled", "detached", "legacy subscription", "legacy exclusive", "invalid type", "wrong platform", "legacy ordinary", "free", "discount", "image discount", "cooldown", "read failed"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, repo, resolve := sharedAdmissionFixture()
			selected := *repo.account
			group := ctx.Value(ctxkey.Group).(*Group)
			switch scenario {
			case "paused":
				repo.account.Extra[SharedPoolEnabledKey] = false
			case "admin disabled":
				repo.account.Extra[SharedPoolAdminDisabledKey] = true
			case "detached":
				repo.account.GroupIDs = nil
			case "legacy subscription":
				delete(repo.account.Extra, SharedPoolDispatchConsentKey)
				group.IsSharedPool = true
				group.SubscriptionType = SubscriptionTypeSubscription
			case "legacy exclusive":
				delete(repo.account.Extra, SharedPoolDispatchConsentKey)
				group.IsSharedPool = true
				group.IsExclusive = true
			case "invalid type":
				group.SubscriptionType = "unsupported"
			case "wrong platform":
				group.Platform = PlatformGemini
			case "legacy ordinary":
				delete(repo.account.Extra, SharedPoolDispatchConsentKey)
			case "free":
				resolve = func(context.Context, int64, int64, float64) float64 { return 0 }
			case "discount":
				resolve = func(context.Context, int64, int64, float64) float64 { return .4 }
			case "image discount":
				group.ImageRateIndependent, group.ImageRateMultiplier = true, .2
				ctx = context.WithValue(ctx, ctxkey.OpenAIImageGenerationIntent, true)
			case "cooldown":
				until := time.Now().Add(time.Hour)
				repo.account.TempUnschedulableUntil = &until
			case "read failed":
				repo.err = errors.New("unavailable")
			}
			_, vetoed, reason := sharedPoolAdmissionLatest(ctx, &selected, repo, resolve)
			require.True(t, vetoed, reason)
		})
	}
}

func TestSharedPoolAdmissionPreservesLegacyAndOrdinaryAccounts(t *testing.T) {
	ctx, repo, resolve := sharedAdmissionFixture()
	delete(repo.account.Extra, SharedPoolDispatchConsentKey)
	ctx.Value(ctxkey.Group).(*Group).IsSharedPool = true
	latest, vetoed, _ := sharedPoolAdmissionLatest(ctx, repo.account, repo, resolve)
	require.False(t, vetoed)
	require.Nil(t, latest.SharedPoolSettlement)
	require.Zero(t, repo.groupReads, "legacy request keeps its previous group behavior")
	ordinary := &Account{ID: 22}
	got, vetoed, _ := sharedPoolAdmissionLatest(context.Background(), ordinary, nil, nil)
	require.False(t, vetoed)
	require.Same(t, ordinary, got)
}

func TestSharedPoolAdmissionRereadsGroupForEachWebSocketTurn(t *testing.T) {
	for _, scenario := range []string{"disabled", "lower token price", "lower image price"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, repo, _ := sharedAdmissionFixture()
			connectedGroup := ctx.Value(ctxkey.Group).(*Group)
			connectedGroup.ImageRateIndependent, connectedGroup.ImageRateMultiplier = true, 1
			ctx = context.WithValue(ctx, ctxkey.OpenAIImageGenerationIntent, true)
			resolve := func(_ context.Context, _, _ int64, base float64) float64 { return base }
			first, vetoed, reason := sharedPoolAdmissionLatest(ctx, repo.account, repo, resolve)
			require.False(t, vetoed, reason)
			require.Equal(t, 2.0, first.SharedPoolSettlement.ConsumerTokenMultiplier)
			require.Equal(t, 1.0, first.SharedPoolSettlement.ConsumerImageMultiplier)
			changed := *connectedGroup
			switch scenario {
			case "disabled":
				changed.Status = StatusDisabled
			case "lower token price":
				changed.RateMultiplier = .4
			case "lower image price":
				changed.ImageRateMultiplier = .4
			}
			repo.group = &changed
			_, vetoed, reason = sharedPoolAdmissionLatest(ctx, repo.account, repo, resolve)
			require.True(t, vetoed, reason)
			require.Equal(t, 2, repo.groupReads)
			require.Equal(t, StatusActive, connectedGroup.Status)
			require.Equal(t, 2.0, connectedGroup.RateMultiplier)
			require.Equal(t, 2.0, first.SharedPoolSettlement.ConsumerTokenMultiplier)
			require.Equal(t, 1.0, first.SharedPoolSettlement.ConsumerImageMultiplier)
		})
	}
}

func TestSharedPoolAdmissionFreezesLatestGroupPricesWithoutChangingEarlierTurn(t *testing.T) {
	ctx, repo, _ := sharedAdmissionFixture()
	resolve := func(_ context.Context, _, _ int64, base float64) float64 { return base }
	first, vetoed, _ := sharedPoolAdmissionLatest(ctx, repo.account, repo, resolve)
	require.False(t, vetoed)
	changed := *repo.group
	changed.RateMultiplier, changed.ImageRateIndependent, changed.ImageRateMultiplier = 1.5, true, .75
	repo.group = &changed
	second, vetoed, _ := sharedPoolAdmissionLatest(ctx, repo.account, repo, resolve)
	require.False(t, vetoed)
	require.Equal(t, 2.0, first.SharedPoolSettlement.ConsumerTokenMultiplier)
	require.Equal(t, 2.0, first.SharedPoolSettlement.ConsumerImageMultiplier)
	require.Equal(t, 1.5, second.SharedPoolSettlement.ConsumerTokenMultiplier)
	require.Equal(t, .75, second.SharedPoolSettlement.ConsumerImageMultiplier)
	require.Zero(t, repo.terms.ConsumerTokenMultiplier)
}

func TestSharedPoolAdmissionFailsClosedWhenLatestGroupUnavailable(t *testing.T) {
	for _, scenario := range []string{"query failed", "deleted", "wrong group", "unsupported repository", "missing context group"} {
		t.Run(scenario, func(t *testing.T) {
			ctx, repo, resolve := sharedAdmissionFixture()
			var source AccountRepository = repo
			switch scenario {
			case "query failed":
				repo.groupErr = errors.New("group lookup failed")
			case "deleted":
				repo.group = nil
			case "wrong group":
				repo.group = &Group{ID: 99}
			case "unsupported repository":
				source = struct{ AccountRepository }{repo}
			case "missing context group":
				ctx = context.Background()
			}
			_, vetoed, reason := sharedPoolAdmissionLatest(ctx, repo.account, source, resolve)
			require.True(t, vetoed)
			require.Equal(t, "shared_group_unavailable", reason)
		})
	}
}
