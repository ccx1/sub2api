//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerCacheCodexQuotaIdentityAdmission(t *testing.T) {
	identities := []struct {
		credentialKey string
		snapshotKey   string
	}{
		{"email", "email"},
		{"email", "email_address"},
		{"chatgpt_account_id", "chatgpt_account_id"},
		{"chatgpt_account_id", "account_id"},
		{"workspace_id", "workspace_id"},
		{"chatgpt_workspace_id", "chatgpt_workspace_id"},
		{"organization_id", "organization_id"},
		{"org_id", "org_id"},
	}
	for _, identity := range identities {
		t.Run(identity.credentialKey+"/"+identity.snapshotKey, func(t *testing.T) {
			for _, matches := range []bool{true, false} {
				name := "matching"
				if !matches {
					name = "conflicting"
				}
				t.Run(name, func(t *testing.T) {
					verifySchedulerCodexQuotaIdentity(t, identity.credentialKey, identity.snapshotKey, matches)
				})
			}
		})
	}
}

func verifySchedulerCodexQuotaIdentity(t *testing.T, credentialKey, snapshotKey string, matches bool) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Second)
	identity := "current-identity"
	if !matches {
		identity = "previous-identity"
	}
	account := service.Account{
		ID: 71, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{
			credentialKey: "current-identity", "access_token": "private-access",
			"refresh_token": "private-refresh", "id_token": "private-id", "client_secret": "private-client",
		},
		Extra: map[string]any{
			snapshotKey: identity, "codex_5h_used_percent": 100.0,
			"codex_5h_reset_at":      now.Add(time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at": now.Format(time.RFC3339), "unrelated_private_payload": "drop-me",
		},
	}
	cache := newSchedulerCacheUnit(t)
	ctx := context.Background()
	bucket := service.SchedulerBucket{GroupID: 7, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	token, err := cache.CaptureBucketWriteToken(ctx, bucket)
	require.NoError(t, err)
	require.NoError(t, cache.SetSnapshot(ctx, bucket, token, []service.Account{account}))
	candidates, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, candidates, 1)
	full, err := cache.GetAccount(ctx, account.ID)
	require.NoError(t, err)
	require.NotNil(t, full)
	metadata := candidates[0]
	require.Equal(t, "current-identity", metadata.Credentials[credentialKey])
	require.Equal(t, identity, metadata.Extra[snapshotKey])
	for _, key := range []string{"access_token", "refresh_token", "id_token", "client_secret"} {
		require.NotContains(t, metadata.Credentials, key)
	}
	require.NotContains(t, metadata.Extra, "unrelated_private_payload")
	for _, candidate := range []*service.Account{&account, full, metadata} {
		require.Equal(t, matches, candidate.IsOpenAICodexQuotaExhausted(now))
		require.Equal(t, !matches, candidate.IsSchedulable())
	}
}
