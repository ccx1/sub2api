//go:build unit

package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestGrokUnknownForbiddenPolicy(t *testing.T) {
	account := &Account{ID: 98901, Platform: PlatformGrok, Type: AccountTypeOAuth}
	unknown := []byte(`{"error":{"message":"Forbidden"}}`)
	require.False(t, account.SkipGrokForbiddenPause())
	account.Extra = map[string]any{"grok_skip_forbidden_pause": "true"}
	require.False(t, account.SkipGrokForbiddenPause())
	account.Extra["grok_skip_forbidden_pause"] = true
	err := (&UpstreamFailoverError{StatusCode: 403, ResponseBody: unknown}).WithGrokForbiddenPolicy(account)
	require.Equal(t, GrokUnknownForbiddenReason, err.Reason)
	require.False(t, err.ShouldReportAccountScheduleFailure())
	require.True(t, err.ShouldRetryNextAccount())
	for _, body := range []string{
		`{"error":{"code":"invalid_token"}}`,
		`{"error":{"code":"account_disabled"}}`,
		`{"error":{"message":"subscription required"}}`,
		`{"error":{"message":"spending limit reached"}}`,
		`{"error":{"code":"subscription:free-usage-exhausted"}}`,
		`{"error":{"message":"text is sensitive"}}`,
	} {
		require.False(t, isGrokUnknownForbidden(403, []byte(body)), body)
	}
}

func TestGrokUnknownForbiddenPreservesAccountHealth(t *testing.T) {
	account := &Account{ID: 98902, Platform: PlatformGrok, Type: AccountTypeOAuth,
		Extra: map[string]any{"grok_skip_forbidden_pause": true}}
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	svc.handleGrokAccountUpstreamError(context.Background(), account, 403,
		http.Header{"x-ratelimit-remaining-tokens": {"0"}, "Retry-After": {"5"}}, []byte(`{"error":"Forbidden"}`))
	require.Zero(t, repo.tempUnschedCalls)
	require.Zero(t, repo.rateLimitedCalls)
	require.False(t, svc.isOpenAIAccountRuntimeBlocked(account))
	svc.handleGrokAccountUpstreamError(context.Background(), account, 403, nil, []byte(`{"error":"subscription required"}`))
	require.Equal(t, 1, repo.tempUnschedCalls)
}

func TestGrokModelQuotaShortResetRemainsScoped(t *testing.T) {
	account := &Account{ID: 98903, Platform: PlatformGrok, Type: AccountTypeOAuth}
	repo := &grokQuotaAccountRepo{}
	svc := &OpenAIGatewayService{accountRepo: repo}
	start := time.Now()
	svc.handleGrokAccountUpstreamError(withGrokTeamRateLimitModel(context.Background(), "grok-4.5"), account, 429,
		http.Header{"Retry-After": {"5"}}, []byte(`{"error":{"code":"subscription:free-usage-exhausted","message":"free usage exhausted for model grok-4.5"}}`))
	require.Zero(t, repo.rateLimitedCalls)
	require.Zero(t, repo.tempUnschedCalls)
	globalGrokModelQuotaBlocks.mu.Lock()
	block := globalGrokModelQuotaBlocks.items[grokModelQuotaBlockKey(account.ID, "grok-4.5")]
	globalGrokModelQuotaBlocks.mu.Unlock()
	require.WithinDuration(t, start.Add(5*time.Second), block.Until, time.Second)
	require.False(t, isGrokModelQuotaBlocked(account.ID, "grok-4.6", time.Now()))
}
