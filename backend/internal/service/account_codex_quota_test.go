//go:build unit

package service

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountCodexQuotaExhaustionBoundaries(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	tests := []struct {
		name    string
		window  string
		used    float64
		updated string
		reset   string
		blocked bool
	}{
		{"weekly exhausted", "7d", 100, now.Format(time.RFC3339), now.Add(time.Hour).Format(time.RFC3339), true},
		{"below exhaustion", "5h", 99.9, now.Format(time.RFC3339), "", false},
		{"missing sample and reset", "5h", 100, "", "", false},
		{"invalid sample without reset", "5h", 100, "invalid", "", false},
		{"future sample without reset", "5h", 100, now.Add(time.Hour).Format(time.RFC3339), "", false},
		{"stale sample despite future reset", "7d", 100, now.Add(-2 * time.Hour).Format(time.RFC3339), now.Add(time.Hour).Format(time.RFC3339), false},
		{"reset reached", "5h", 100, now.Add(-time.Minute).Format(time.RFC3339), now.Format(time.RFC3339), false},
		{"nonfinite percentage", "5h", math.NaN(), now.Format(time.RFC3339), "", false},
		{"infinite percentage", "7d", math.Inf(1), now.Format(time.RFC3339), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
				Extra: map[string]any{
					"codex_" + tt.window + "_used_percent": tt.used,
					"codex_" + tt.window + "_reset_at":     tt.reset,
					"codex_usage_updated_at":               tt.updated,
				},
			}
			require.Equal(t, tt.blocked, account.IsOpenAICodexQuotaExhausted(now))
			require.Equal(t, !tt.blocked, account.isSchedulableAt(now))
		})
	}
}

func TestRateLimitService_ExhaustedCodexPreservesShadowCredentialAndRecovery(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	account := &Account{ID: 78, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Extra: map[string]any{
			"codex_5h_used_percent":  100.0,
			"codex_5h_reset_at":      now.Add(time.Hour).Format(time.RFC3339),
			"codex_usage_updated_at": now.Format(time.RFC3339),
		},
	}
	repo := &rateLimitAccountRepoStub{}
	svc := &RateLimitService{accountRepo: repo}
	require.True(t, svc.ApplyAccountSchedulingThreshold(context.Background(), account))
	require.False(t, account.IsSchedulable())
	require.True(t, account.IsCredentialUsableForShadow())
	require.True(t, account.Schedulable)
	require.Zero(t, repo.tempCalls)
	require.Nil(t, account.TempUnschedulableUntil)
	require.Nil(t, account.RateLimitResetAt)
	require.True(t, account.isSchedulableAt(now.Add(time.Hour)))
}

func TestOpenAIGatewayServiceIsAccountSchedulableNowHandlesRecoveryAndReadFailure(t *testing.T) {
	selected := &Account{ID: 79, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		Extra: map[string]any{"codex_5h_used_percent": 100.0, "codex_5h_reset_at": time.Now().Add(time.Hour).Format(time.RFC3339)},
	}
	fresh := *selected
	fresh.Extra = map[string]any{"codex_5h_used_percent": 0.0}
	svc := &OpenAIGatewayService{accountRepo: stubOpenAIAccountRepo{accounts: []Account{fresh}}}
	require.True(t, svc.IsAccountSchedulableNow(context.Background(), selected))
	svc.accountRepo = stubOpenAIAccountRepo{}
	require.False(t, svc.IsAccountSchedulableNow(context.Background(), selected))
	require.False(t, svc.IsAccountSchedulableNow(context.Background(), nil))
}

func TestCodexUsageSnapshotExhaustionUntilAnchorsSampleAndExpires(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	used, reset := 100.0, 24*60*60
	snapshot := &OpenAICodexUsageSnapshot{
		PrimaryUsedPercent: &used, PrimaryResetAfterSeconds: &reset,
		UpdatedAt: now.Add(-time.Hour).Format(time.RFC3339),
	}
	until, exhausted := CodexUsageSnapshotExhaustionUntil(snapshot, now)
	require.True(t, exhausted)
	require.Equal(t, now.Add(time.Hour), until)
	_, exhausted = CodexUsageSnapshotExhaustionUntil(snapshot, until)
	require.False(t, exhausted)
	snapshot.PrimaryResetAfterSeconds = nil
	until, exhausted = CodexUsageSnapshotExhaustionUntil(snapshot, now)
	require.True(t, exhausted)
	require.Equal(t, now.Add(time.Hour), until)
}
