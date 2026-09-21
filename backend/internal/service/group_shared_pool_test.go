package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type sharedPoolGroupRepoStub struct {
	GroupRepository
	groups    []Group
	available map[int64]bool
	err       error
	written   *Group
	bound     bool
}

func (r *sharedPoolGroupRepoStub) ListActive(context.Context) ([]Group, error) { return r.groups, nil }
func (r *sharedPoolGroupRepoStub) SharedPoolAvailableGroupIDs(context.Context, []int64) (map[int64]bool, error) {
	return r.available, r.err
}

func (r *sharedPoolGroupRepoStub) HasSharedPoolAccounts(context.Context, int64) (bool, error) {
	return r.bound, nil
}

func (r *sharedPoolGroupRepoStub) UpdateWithoutSharedPool(ctx context.Context, group *Group) error {
	return r.Update(ctx, group)
}
func (r *sharedPoolGroupRepoStub) Create(_ context.Context, group *Group) error {
	r.written = group
	return nil
}
func (r *sharedPoolGroupRepoStub) GetByID(context.Context, int64) (*Group, error) {
	copy := r.groups[0]
	return &copy, nil
}
func (r *sharedPoolGroupRepoStub) Update(_ context.Context, group *Group) error {
	r.written = group
	return nil
}

type sharedPoolUserRepoStub struct {
	UserRepository
	user *User
}

func (r *sharedPoolUserRepoStub) GetByID(context.Context, int64) (*User, error) { return r.user, nil }

type sharedPoolSubRepoStub struct{ UserSubscriptionRepository }

func (*sharedPoolSubRepoStub) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return nil, nil
}

func sharedPoolTestGroup() Group {
	return Group{ID: 1, Name: "shared", Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, IsSharedPool: true}
}

func TestSharedPoolLegacyMarkerUsesOrdinaryGroupPermissions(t *testing.T) {
	ctx := context.Background()
	repo := &sharedPoolGroupRepoStub{groups: []Group{sharedPoolTestGroup()}, available: map[int64]bool{1: true}}
	user := &User{ID: 7, RestrictPublicGroups: true}
	svc := &APIKeyService{groupRepo: repo, userRepo: &sharedPoolUserRepoStub{user: user}, userSubRepo: &sharedPoolSubRepoStub{}}
	repo.err = errors.New("retired availability must not be queried")
	for _, allowed := range []bool{false, true, false} {
		user.AllowedGroups = nil
		if allowed {
			user.AllowedGroups = []int64{1}
		}
		groups, err := svc.GetAvailableGroups(ctx, user.ID)
		require.NoError(t, err)
		require.Equal(t, allowed, len(groups) == 1)
		require.Equal(t, allowed, svc.canUserBindGroup(ctx, user, &repo.groups[0]))
	}
}

func TestSharedPoolGroupCreationAndEnablingAreRetired(t *testing.T) {
	group := sharedPoolTestGroup()
	group.IsSharedPool = false
	repo := &sharedPoolGroupRepoStub{groups: []Group{group}}
	svc := &adminServiceImpl{groupRepo: repo}
	_, err := svc.CreateGroup(context.Background(), &CreateGroupInput{Name: "pool", Platform: PlatformOpenAI, IsSharedPool: true, RateMultiplier: 1})
	require.ErrorContains(t, err, "共享分组")
	enabled := true
	_, err = svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{IsSharedPool: &enabled})
	require.ErrorContains(t, err, "共享分组")
	require.Nil(t, repo.written)
}

func TestSharedPoolLegacyMarkerDoesNotBlockOrdinaryGroupEditing(t *testing.T) {
	repo := &sharedPoolGroupRepoStub{groups: []Group{sharedPoolTestGroup()}}
	svc := &adminServiceImpl{groupRepo: repo}
	exclusive := true
	actual, err := svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{IsExclusive: &exclusive})
	require.NoError(t, err)
	require.True(t, actual.IsExclusive)
	require.True(t, actual.IsSharedPool, "保留旧账号授权标记，普通编辑不改变历史授权")
}

func TestSharedPoolLegacyMarkerIsNotCopiedToNewGroups(t *testing.T) {
	group := sharedPoolTestGroup()
	cloned := cloneGroupForDuplicate(&group, "test-copy")
	require.False(t, cloned.IsSharedPool)
	require.True(t, group.IsSharedPool)
}

func TestSharedPoolAccountAvailabilityUsesSchedulingAndGroupPolicies(t *testing.T) {
	future := time.Now().Add(time.Hour)
	group := sharedPoolTestGroup()
	for _, tc := range []struct {
		name   string
		change func(*Group, *Account)
	}{
		{"disabled account", func(_ *Group, a *Account) { a.Schedulable = false }},
		{"inactive account", func(_ *Group, a *Account) { a.Status = StatusDisabled }},
		{"cooldown", func(_ *Group, a *Account) { a.TempUnschedulableUntil = &future }},
		{"rate limit", func(_ *Group, a *Account) { a.RateLimitResetAt = &future }},
		{"platform mismatch", func(_ *Group, a *Account) { a.Platform = PlatformAnthropic }},
		{"oauth only", func(g *Group, a *Account) { g.RequireOAuthOnly = true; a.Type = AccountTypeAPIKey }},
		{"privacy required", func(g *Group, _ *Account) { g.RequirePrivacySet = true }},
		{"disabled group", func(g *Group, _ *Account) { g.Status = StatusDisabled }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := group
			acc := Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
			require.True(t, IsSharedPoolAccountAvailable(&g, &acc))
			tc.change(&g, &acc)
			require.False(t, IsSharedPoolAccountAvailable(&g, &acc))
		})
	}
}

func TestSharedPoolAuthSnapshotPreservesLegacyMarker(t *testing.T) {
	svc := &APIKeyService{}
	group := sharedPoolTestGroup()
	key := &APIKey{ID: 1, User: &User{ID: 7, RestrictPublicGroups: true}, Group: &group, GroupID: &group.ID}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	require.True(t, snapshot.Group.IsSharedPool)
	loaded, ok, err := svc.applyAuthCacheEntry("shared-pool-test-key", &APIKeyAuthCacheEntry{Snapshot: snapshot})
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, loaded.Group.IsSharedPool)
}

func TestSharedPoolCannotRemoveSharedFlagBeforeMigratingAccounts(t *testing.T) {
	repo := &sharedPoolGroupRepoStub{groups: []Group{sharedPoolTestGroup()}, bound: true}
	svc := &adminServiceImpl{groupRepo: repo}
	disabled := false
	_, err := svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{IsSharedPool: &disabled})
	require.Error(t, err)
	require.Nil(t, repo.written)
	repo.bound = false
	actual, err := svc.UpdateGroup(context.Background(), 1, &UpdateGroupInput{IsSharedPool: &disabled})
	require.NoError(t, err)
	require.False(t, actual.IsSharedPool)
}

type sharedPoolCapacityRepoStub struct {
	*sharedPoolGroupRepoStub
	capacities map[int64]SharedPoolCapacity
}

func (r *sharedPoolCapacityRepoStub) SharedPoolAvailableCapacities(context.Context, []int64) (map[int64]SharedPoolCapacity, error) {
	return r.capacities, r.err
}

func TestSharedPoolLegacyGroupsDoNotDependOnAccountAvailability(t *testing.T) {
	first, second, empty := sharedPoolTestGroup(), sharedPoolTestGroup(), sharedPoolTestGroup()
	second.ID, empty.ID = 2, 3
	repo := &sharedPoolCapacityRepoStub{sharedPoolGroupRepoStub: &sharedPoolGroupRepoStub{groups: []Group{first, second, empty}},
		capacities: map[int64]SharedPoolCapacity{1: {AvailableAccounts: 2, ConcurrencyCapacity: 8}, 2: {AvailableAccounts: 1, ConcurrencyUnlimited: true}}}
	svc := &APIKeyService{groupRepo: repo, userRepo: &sharedPoolUserRepoStub{user: &User{ID: 7}}, userSubRepo: &sharedPoolSubRepoStub{}}
	groups, err := svc.GetAvailableGroups(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, groups, 3, "消费分组可见性不再由供号资源概览决定")
	require.Nil(t, groups[0].SharedPoolCapacity)
	repo.err = errors.New("capacity unavailable")
	_, err = svc.GetAvailableGroups(context.Background(), 7)
	require.NoError(t, err)
}
