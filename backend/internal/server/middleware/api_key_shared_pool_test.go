package middleware

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolLegacyMarkerDoesNotBypassGroupAuthorization(t *testing.T) {
	id := int64(12)
	key := &service.APIKey{GroupID: &id, User: &service.User{RestrictPublicGroups: true}, Group: &service.Group{
		ID: id, IsSharedPool: true, Platform: service.PlatformOpenAI, SubscriptionType: service.SubscriptionTypeStandard,
	}}
	require.False(t, validateAPIKeyGroupAllowed(key))
	key.Group.IsExclusive = true
	require.False(t, validateAPIKeyGroupAllowed(key))
	key.User.AllowedGroups = []int64{id}
	require.True(t, validateAPIKeyGroupAllowed(key))
	key.User.RestrictPublicGroups = false
	key.User.AllowedGroups = nil
	key.Group.IsExclusive = false
	require.True(t, validateAPIKeyGroupAllowed(key))
}
