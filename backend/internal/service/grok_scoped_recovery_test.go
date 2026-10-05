//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type grokModelRecoveryRepoStub struct {
	rateLimitClearRepoStub
	onClear func()
}

func (r *grokModelRecoveryRepoStub) ClearGrokModelRateLimitIfUnchanged(context.Context, int64, string, map[string]any) (bool, error) {
	if r.onClear != nil {
		r.onClear()
	}
	return true, nil
}

func TestGrokSuccessfulTestRecoveryOnlyClearsTestedModel(t *testing.T) {
	account := &Account{ID: 98904, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"team_id": "grok-recovery-test-team"}}
	repo := &rateLimitClearRepoStub{getByIDAccount: account}
	svc := &RateLimitService{accountRepo: repo}
	until := time.Now().Add(time.Minute)
	for _, model := range []string{"grok-4.5", "grok-4.6"} {
		markGrokModelQuotaBlock(account.ID, model, until)
		markGrokTeamModelRateLimit(account, model, until)
	}
	ctx, err := svc.WithSuccessfulTestRecoverySnapshot(context.Background(), account.ID, "grok-4.5")
	require.NoError(t, err)
	result, err := svc.RecoverGrokAccountAfterSuccessfulTest(ctx, account.ID, "grok-4.5")
	require.NoError(t, err)
	require.True(t, result.ClearedRateLimit)
	require.False(t, isGrokModelQuotaBlocked(account.ID, "grok-4.5", time.Now()))
	require.False(t, isGrokTeamModelRateLimited(account, "grok-4.5", time.Now()))
	require.True(t, isGrokModelQuotaBlocked(account.ID, "grok-4.6", time.Now()))
	require.True(t, isGrokTeamModelRateLimited(account, "grok-4.6", time.Now()))
	require.Zero(t, repo.clearRateLimitCalls)
	require.Zero(t, repo.clearTempUnschedCalls)
	account.Status = StatusDisabled
	_, err = svc.RecoverGrokAccountAfterSuccessfulTest(context.Background(), account.ID, "grok-4.6")
	require.NoError(t, err)
	require.True(t, isGrokModelQuotaBlocked(account.ID, "grok-4.6", time.Now()))
}

func TestGrokSuccessfulTestRecoveryKeepsConcurrentSameDeadlineFailure(t *testing.T) {
	account := &Account{ID: 98905, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"team_id": "grok-recovery-cas-team"},
		Extra:       map[string]any{"model_rate_limits": map[string]any{"grok-4.5": map[string]any{"reason": "grok model quota exhausted"}}}}
	until := time.Now().Add(time.Minute)
	repo := &grokModelRecoveryRepoStub{rateLimitClearRepoStub: rateLimitClearRepoStub{getByIDAccount: account}, onClear: func() {
		markGrokModelQuotaBlock(account.ID, "grok-4.5", until)
		markGrokTeamModelRateLimit(account, "grok-4.5", until)
	}}
	markGrokModelQuotaBlock(account.ID, "grok-4.5", until)
	markGrokTeamModelRateLimit(account, "grok-4.5", until)
	svc := &RateLimitService{accountRepo: repo}
	ctx, err := svc.WithSuccessfulTestRecoverySnapshot(context.Background(), account.ID, "grok-4.5")
	require.NoError(t, err)
	_, err = svc.RecoverGrokAccountAfterSuccessfulTest(ctx, account.ID, "grok-4.5")
	require.NoError(t, err)
	require.True(t, isGrokModelQuotaBlocked(account.ID, "grok-4.5", time.Now()))
	require.True(t, isGrokTeamModelRateLimited(account, "grok-4.5", time.Now()))
}

func TestGrokSuccessfulTestRecoveryKeepsFailureDuringProbe(t *testing.T) {
	account := &Account{ID: 98907, Platform: PlatformGrok, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
	svc := &RateLimitService{accountRepo: &rateLimitClearRepoStub{getByIDAccount: account}}
	ctx, err := svc.WithSuccessfulTestRecoverySnapshot(context.Background(), account.ID, "grok-4.5")
	require.NoError(t, err)
	markGrokModelQuotaBlock(account.ID, "grok-4.5", time.Now().Add(time.Minute))
	result, err := svc.RecoverAccountAfterSuccessfulTestForModel(ctx, account.ID, "grok-4.5")
	require.NoError(t, err)
	require.False(t, result.ClearedRateLimit)
	require.True(t, isGrokModelQuotaBlocked(account.ID, "grok-4.5", time.Now()))
}
