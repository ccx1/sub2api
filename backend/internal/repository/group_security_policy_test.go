package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGroupSecurityPolicyPersistsAndLoadsForAuthentication(t *testing.T) {
	keyRepo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	groups := newGroupRepositoryWithSQL(client, nil)
	group := &service.Group{Name: "security-policy", Platform: service.PlatformOpenAI,
		Status: service.StatusActive, SubscriptionType: service.SubscriptionTypeStandard,
		SecurityPolicyEnabled: true, SecurityPolicyMode: service.SecurityPolicyModeBlockRequest,
		SecurityPolicyEmailEnabled: true}
	require.NoError(t, groups.Create(ctx, group))
	created, err := groups.GetByIDLite(ctx, group.ID)
	require.NoError(t, err)
	require.True(t, created.SecurityPolicyEnabled)
	require.Equal(t, service.SecurityPolicyModeBlockRequest, created.SecurityPolicyMode)
	require.True(t, created.SecurityPolicyEmailEnabled)

	created.SecurityPolicyMode = service.SecurityPolicyModeBlockSession
	created.SecurityPolicyEmailEnabled = false
	require.NoError(t, groups.Update(ctx, created))
	u := mustCreateAPIKeyRepoUser(t, ctx, client, "security-policy-test@example.com")
	key := &service.APIKey{UserID: u.ID, Key: "security-policy-test-key", Name: "security-policy-key", GroupID: &group.ID, Status: service.StatusActive}
	require.NoError(t, keyRepo.Create(ctx, key))
	loaded, err := keyRepo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.True(t, loaded.Group.SecurityPolicyEnabled)
	require.Equal(t, service.SecurityPolicyModeBlockSession, loaded.Group.SecurityPolicyMode)
	require.False(t, loaded.Group.SecurityPolicyEmailEnabled)

	created.SecurityPolicyEnabled = false
	require.NoError(t, groups.Update(ctx, created))
	loaded, err = keyRepo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.False(t, loaded.Group.SecurityPolicyEnabled)
}
