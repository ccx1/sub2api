package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityPolicySurvivesAuthCacheRoundTrip(t *testing.T) {
	svc := &APIKeyService{}
	key := &APIKey{ID: 1, UserID: 2, Status: StatusAPIKeyActive, User: &User{ID: 2, Status: StatusActive},
		Group: &Group{ID: 3, SecurityPolicyEnabled: true, SecurityPolicyMode: SecurityPolicyModeBlockRequest, SecurityPolicyEmailEnabled: true}}
	snapshot := svc.snapshotFromAPIKey(context.Background(), key)
	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	var restored APIKeyAuthSnapshot
	require.NoError(t, json.Unmarshal(raw, &restored))
	got := svc.snapshotToAPIKey("test-key", &restored)
	require.True(t, got.Group.SecurityPolicyEnabled)
	require.Equal(t, SecurityPolicyModeBlockRequest, got.Group.SecurityPolicyMode)
	require.True(t, got.Group.SecurityPolicyEmailEnabled)
}

func TestSecurityPolicyRejectsPrePolicyAuthCacheSnapshot(t *testing.T) {
	key, hit, err := (&APIKeyService{}).applyAuthCacheEntry("test-key", &APIKeyAuthCacheEntry{
		Snapshot: &APIKeyAuthSnapshot{Version: 24},
	})
	require.NoError(t, err)
	require.False(t, hit)
	require.Nil(t, key)
}
