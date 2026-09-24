//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountIsSchedulable_QuotaExceeded(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		account *Account
		want    bool
	}{
		{
			name: "apikey daily quota exceeded",
			account: &Account{
				Status:      StatusActive,
				Schedulable: true,
				Type:        AccountTypeAPIKey,
				Extra: map[string]any{
					"quota_daily_limit": 10.0,
					"quota_daily_used":  10.0,
					"quota_daily_start": now.Add(-1 * time.Hour).Format(time.RFC3339),
				},
			},
			want: false,
		},
		{
			name: "apikey weekly quota exceeded",
			account: &Account{
				Status:      StatusActive,
				Schedulable: true,
				Type:        AccountTypeAPIKey,
				Extra: map[string]any{
					"quota_weekly_limit": 50.0,
					"quota_weekly_used":  50.0,
					"quota_weekly_start": now.Add(-2 * 24 * time.Hour).Format(time.RFC3339),
				},
			},
			want: false,
		},
		{
			name: "apikey total quota exceeded",
			account: &Account{
				Status:      StatusActive,
				Schedulable: true,
				Type:        AccountTypeAPIKey,
				Extra: map[string]any{
					"quota_limit": 100.0,
					"quota_used":  100.0,
				},
			},
			want: false,
		},
		{
			name: "apikey quota not exceeded",
			account: &Account{
				Status:      StatusActive,
				Schedulable: true,
				Type:        AccountTypeAPIKey,
				Extra: map[string]any{
					"quota_daily_limit": 10.0,
					"quota_daily_used":  5.0,
					"quota_daily_start": now.Add(-1 * time.Hour).Format(time.RFC3339),
				},
			},
			want: true,
		},
		{
			name: "apikey expired daily period restores schedulable",
			account: &Account{
				Status:      StatusActive,
				Schedulable: true,
				Type:        AccountTypeAPIKey,
				Extra: map[string]any{
					"quota_daily_limit": 10.0,
					"quota_daily_used":  10.0,
					"quota_daily_start": now.Add(-25 * time.Hour).Format(time.RFC3339),
				},
			},
			want: true,
		},
		{
			name: "oauth ignores quota exceeded",
			account: &Account{
				Status:      StatusActive,
				Schedulable: true,
				Type:        AccountTypeOAuth,
				Extra: map[string]any{
					"quota_daily_limit": 10.0,
					"quota_daily_used":  10.0,
					"quota_daily_start": now.Add(-1 * time.Hour).Format(time.RFC3339),
				},
			},
			want: true,
		},
		{
			name: "openai codex fresh exhausted window is unschedulable",
			account: &Account{
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Type:        AccountTypeOAuth,
				Extra: map[string]any{
					"codex_5h_used_percent": 100.0,
					"codex_5h_reset_at":     now.Add(time.Hour).Format(time.RFC3339),
				},
			},
			want: false,
		},
		{
			name: "openai codex exhausted window after reset is schedulable",
			account: &Account{
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Type:        AccountTypeOAuth,
				Extra: map[string]any{
					"codex_5h_used_percent": 100.0,
					"codex_5h_reset_at":     now.Add(-time.Second).Format(time.RFC3339),
				},
			},
			want: true,
		},
		{
			name: "openai codex exhausted window without reset uses fresh sample",
			account: &Account{
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Type:        AccountTypeOAuth,
				Extra: map[string]any{
					"codex_5h_used_percent":  100.0,
					"codex_usage_updated_at": now.Add(-time.Minute).Format(time.RFC3339),
				},
			},
			want: false,
		},
		{
			name: "openai codex exhausted stale sample is schedulable",
			account: &Account{
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Type:        AccountTypeOAuth,
				Extra: map[string]any{
					"codex_5h_used_percent":  100.0,
					"codex_usage_updated_at": now.Add(-3 * time.Hour).Format(time.RFC3339),
				},
			},
			want: true,
		},
		{
			name: "openai codex snapshot from another identity is ignored",
			account: &Account{
				Platform:    PlatformOpenAI,
				Status:      StatusActive,
				Schedulable: true,
				Type:        AccountTypeOAuth,
				Credentials: map[string]any{"email": "current@example.com"},
				Extra: map[string]any{
					"email":                 "previous@example.com",
					"codex_5h_used_percent": 100.0,
					"codex_5h_reset_at":     now.Add(time.Hour).Format(time.RFC3339),
				},
			},
			want: true,
		},
		{
			name: "bedrock quota exceeded",
			account: &Account{
				Status:      StatusActive,
				Schedulable: true,
				Type:        AccountTypeBedrock,
				Extra: map[string]any{
					"quota_limit": 200.0,
					"quota_used":  200.0,
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.account.IsSchedulable())
		})
	}
}

func TestOpenAIGatewayServiceIsAccountSchedulableNowRefreshesQuota(t *testing.T) {
	selected := &Account{ID: 77, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}
	fresh := Account{
		ID: 77, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true,
		Type: AccountTypeOAuth,
		Extra: map[string]any{
			"codex_5h_used_percent": 100.0,
			"codex_5h_reset_at":     time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
		},
	}
	svc := &OpenAIGatewayService{accountRepo: stubOpenAIAccountRepo{accounts: []Account{fresh}}}

	require.False(t, svc.IsAccountSchedulableNow(context.Background(), selected))
}
