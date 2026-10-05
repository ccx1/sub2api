//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type grokRefreshRecoveryRepoStub struct {
	rateLimitClearRepoStub
	beforeCAS func()
	casCalls  int
}

func (r *grokRefreshRecoveryRepoStub) ClearTempUnschedulableIfUnchanged(_ context.Context, _ int64, until time.Time, reason string) (bool, error) {
	r.casCalls++
	if r.beforeCAS != nil {
		r.beforeCAS()
	}
	current := r.getByIDAccount
	if current.Status != StatusActive || !current.Schedulable || current.TempUnschedulableUntil == nil ||
		!current.TempUnschedulableUntil.Equal(until) || current.TempUnschedulableReason != reason {
		return false, nil
	}
	current.TempUnschedulableUntil = nil
	current.TempUnschedulableReason = ""
	return true, nil
}

func grokRefreshRecoveryAccount() *Account {
	until := time.Now().Add(10 * time.Minute)
	return &Account{ID: 98906, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		TempUnschedulableUntil: &until, TempUnschedulableReason: "grok credentials unauthorized"}
}

func TestGrokRefreshRecoveryDoesNotClearConcurrent403(t *testing.T) {
	current := grokRefreshRecoveryAccount()
	observed := *current
	repo := &grokRefreshRecoveryRepoStub{rateLimitClearRepoStub: rateLimitClearRepoStub{getByIDAccount: current}}
	repo.beforeCAS = func() { current.TempUnschedulableReason = "grok access or entitlement denied" }
	svc := &TokenRefreshService{accountRepo: repo}
	cleared, err := svc.recoverGrokRefreshCooldown(context.Background(), &observed)
	require.NoError(t, err)
	require.False(t, cleared)
	require.NotNil(t, current.TempUnschedulableUntil)
	require.Equal(t, "grok access or entitlement denied", current.TempUnschedulableReason)
	require.Zero(t, repo.clearTempUnschedCalls)
}

func TestGrokRefreshRecoveryRuntimeGenerationFencesNew403(t *testing.T) {
	current := grokRefreshRecoveryAccount()
	observed := *current
	gateway := &OpenAIGatewayService{}
	gateway.BlockAccountScheduling(current, *current.TempUnschedulableUntil, current.TempUnschedulableReason)
	repo := &grokRefreshRecoveryRepoStub{rateLimitClearRepoStub: rateLimitClearRepoStub{getByIDAccount: current}}
	svc := &TokenRefreshService{accountRepo: repo, runtimeBlocker: gateway}
	ctx := svc.withGrokRefreshRuntimeSnapshot(context.Background(), &observed)
	// A new writer can install an equal deadline before its DB write lands.
	repo.beforeCAS = func() { gateway.BlockAccountScheduling(&observed, *observed.TempUnschedulableUntil, "new 403") }
	cleared, err := svc.recoverGrokRefreshCooldown(ctx, &observed)
	require.NoError(t, err)
	require.True(t, cleared)
	require.True(t, gateway.peekOpenAIAccountRuntimeBlock(&observed).blocked)
}

func TestGrokRefreshRecoveryOnlyClearsOriginal401(t *testing.T) {
	for _, reason := range []string{"grok access or entitlement denied", "grok free usage exhausted", `{"status_code":403}`, "grok upstream temporary error"} {
		account := grokRefreshRecoveryAccount()
		account.TempUnschedulableReason = reason
		repo := &grokRefreshRecoveryRepoStub{rateLimitClearRepoStub: rateLimitClearRepoStub{getByIDAccount: account}}
		cleared, err := (&TokenRefreshService{accountRepo: repo}).recoverGrokRefreshCooldown(context.Background(), account)
		require.NoError(t, err)
		require.False(t, cleared, reason)
		require.Zero(t, repo.casCalls)
	}
	account := grokRefreshRecoveryAccount()
	repo := &grokRefreshRecoveryRepoStub{rateLimitClearRepoStub: rateLimitClearRepoStub{getByIDAccount: account}}
	cleared, err := (&TokenRefreshService{accountRepo: repo}).recoverGrokRefreshCooldown(context.Background(), account)
	require.NoError(t, err)
	require.True(t, cleared)
	require.Nil(t, account.TempUnschedulableUntil)
	account = grokRefreshRecoveryAccount()
	account.Status = StatusDisabled
	repo.getByIDAccount = account
	cleared, err = (&TokenRefreshService{accountRepo: repo}).recoverGrokRefreshCooldown(context.Background(), account)
	require.NoError(t, err)
	require.False(t, cleared)
}

func TestGrokRefreshRecoveryWithoutConditionalCacheRetainsProtection(t *testing.T) {
	account := grokRefreshRecoveryAccount()
	repo := &grokRefreshRecoveryRepoStub{rateLimitClearRepoStub: rateLimitClearRepoStub{getByIDAccount: account}}
	cache := &tempUnschedCacheRecorder{}
	cleared, err := (&TokenRefreshService{accountRepo: repo, tempUnschedCache: cache}).recoverGrokRefreshCooldown(context.Background(), account)
	require.NoError(t, err)
	require.False(t, cleared)
	require.Zero(t, repo.casCalls)
	require.Empty(t, cache.deletedIDs)
}
