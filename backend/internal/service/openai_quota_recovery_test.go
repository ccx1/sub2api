package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type quotaRecoveryTestRepo struct {
	AccountRepository
	mu       sync.Mutex
	account  Account
	writes   int
	clears   int
	writeErr error
	clearErr error
}

func (r *quotaRecoveryTestRepo) GetByID(context.Context, int64) (*Account, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.account
	return &a, nil
}
func (r *quotaRecoveryTestRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.writes++
	if r.writeErr != nil {
		return r.writeErr
	}
	r.account.Extra = updates
	return nil
}
func (r *quotaRecoveryTestRepo) ClearOpenAIRateLimitIfObserved(_ context.Context, id int64, limitedAt, resetAt time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clears++
	if r.clearErr != nil {
		return false, r.clearErr
	}
	a := &r.account
	if a.ID != id || a.RateLimitedAt == nil || a.RateLimitResetAt == nil || !a.RateLimitedAt.Equal(limitedAt) || !a.RateLimitResetAt.Equal(resetAt) {
		return false, nil
	}
	a.RateLimitedAt, a.RateLimitResetAt = nil, nil
	return true, nil
}
func quotaRecoveryAccount() Account {
	now := time.Now()
	limited, reset := now.Add(-time.Hour), now.Add(4*24*time.Hour)
	return Account{ID: 123, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, RateLimitedAt: &limited, RateLimitResetAt: &reset, Credentials: map[string]any{"chatgpt_account_id": "test-account"}}
}
func zeroQuotaUpdates() map[string]any {
	return map[string]any{"codex_5h_used_percent": float64(0), "codex_7d_used_percent": float64(0), "codex_usage_updated_at": time.Now().UTC().Format(time.RFC3339)}
}

func TestOpenAIQuotaRecoverySnapshotGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		want   bool
	}{
		{"fresh zero", func(map[string]any) {}, true},
		{"partial", func(m map[string]any) { delete(m, "codex_7d_used_percent") }, false},
		{"exhausted", func(m map[string]any) { m["codex_7d_used_percent"] = float64(100) }, false},
		{"nonzero", func(m map[string]any) { m["codex_5h_used_percent"] = 0.1 }, false},
		{"negative", func(m map[string]any) { m["codex_5h_used_percent"] = float64(-1) }, false},
		{"nan", func(m map[string]any) { m["codex_5h_used_percent"] = math.NaN() }, false},
		{"infinite", func(m map[string]any) { m["codex_5h_used_percent"] = math.Inf(1) }, false},
		{"string zero", func(m map[string]any) { m["codex_5h_used_percent"] = "0" }, false},
		{"missing timestamp", func(m map[string]any) { delete(m, "codex_usage_updated_at") }, false},
		{"invalid timestamp", func(m map[string]any) { m["codex_usage_updated_at"] = "bad" }, false},
		{"stale", func(m map[string]any) {
			m["codex_usage_updated_at"] = time.Now().Add(-11 * time.Minute).Format(time.RFC3339)
		}, false},
		{"before 429", func(m map[string]any) {
			m["codex_usage_updated_at"] = time.Now().Add(-2 * time.Hour).Format(time.RFC3339)
		}, false},
		{"future", func(m map[string]any) { m["codex_usage_updated_at"] = time.Now().Add(time.Hour).Format(time.RFC3339) }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &quotaRecoveryTestRepo{account: quotaRecoveryAccount()}
			updates := zeroQuotaUpdates()
			tc.change(updates)
			recoverOpenAIQuotaRateLimit(context.Background(), repo, observeOpenAIQuotaRecovery(&repo.account, time.Now()), updates)
			require.Equal(t, tc.want, repo.account.RateLimitResetAt == nil)
			if !tc.want {
				require.Zero(t, repo.clears)
			}
		})
	}
}

func TestOpenAIQuotaRecoveryObservationGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Account)
	}{
		{"api key", func(a *Account) { a.Type = AccountTypeAPIKey }},
		{"other platform", func(a *Account) { a.Platform = PlatformGrok }},
		{"shadow", func(a *Account) { id := int64(999); a.ParentAccountID = &id }},
		{"no generation", func(a *Account) { a.RateLimitedAt = nil }},
		{"expired", func(a *Account) { ts := time.Now().Add(-time.Minute); a.RateLimitResetAt = &ts }},
		{"future generation", func(a *Account) { ts := time.Now().Add(time.Minute); a.RateLimitedAt = &ts }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := quotaRecoveryAccount()
			tc.change(&a)
			require.Nil(t, observeOpenAIQuotaRecovery(&a, time.Now()))
		})
	}
	require.Nil(t, observeOpenAIQuotaRecovery(nil, time.Now()))
}

const recoveredQuotaBody = "{\"rate_limit\":{\"allowed\":true,\"limit_reached\":false,\"primary_window\":{\"used_percent\":0,\"limit_window_seconds\":18000},\"secondary_window\":{\"used_percent\":0,\"limit_window_seconds\":604800}}}"

func TestOpenAIQuotaRecoveryUsagePayloadPresence(t *testing.T) {
	for _, mode := range []string{"complete", "missing usage", "null usage", "missing allowed", "disallowed", "limited", "missing limit flag", "duplicate window", "unknown window", "nonzero", "empty", "invalid", "swapped"} {
		t.Run(mode, func(t *testing.T) {
			var payload map[string]any
			require.NoError(t, json.Unmarshal([]byte(recoveredQuotaBody), &payload))
			limit, ok := payload["rate_limit"].(map[string]any)
			require.True(t, ok)
			primary, ok := limit["primary_window"].(map[string]any)
			require.True(t, ok)
			switch mode {
			case "missing usage":
				delete(primary, "used_percent")
			case "null usage":
				primary["used_percent"] = nil
			case "missing allowed":
				delete(limit, "allowed")
			case "disallowed":
				limit["allowed"] = false
			case "limited":
				limit["limit_reached"] = true
			case "missing limit flag":
				delete(limit, "limit_reached")
			case "duplicate window":
				primary["limit_window_seconds"] = 604800
			case "unknown window":
				primary["limit_window_seconds"] = 60
			case "nonzero":
				primary["used_percent"] = 1
			case "empty":
				payload = map[string]any{}
			case "swapped":
				limit["primary_window"], limit["secondary_window"] = limit["secondary_window"], limit["primary_window"]
			}
			body, err := json.Marshal(payload)
			require.NoError(t, err)
			if mode == "invalid" {
				body = []byte("{")
			}
			require.Equal(t, mode == "complete" || mode == "swapped", len(openAIQuotaRecoveryUsageUpdates(body, time.Now())) > 0)
		})
	}
}

func TestOpenAIQuotaRecoveryQueryUsage(t *testing.T) {
	for _, mode := range []string{"recovered", "rearmed", "same deadline rearmed", "write failed", "clear failed", "429", "missing data"} {
		t.Run(mode, func(t *testing.T) {
			repo := &quotaRecoveryTestRepo{account: quotaRecoveryAccount()}
			if mode == "write failed" {
				repo.writeErr = errors.New("write failed")
			}
			if mode == "clear failed" {
				repo.clearErr = errors.New("clear failed")
			}
			cache := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(&repo.account): "fake-token"}}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path != "/backend-api/wham/usage" {
					_, _ = w.Write([]byte("{}"))
					return
				}
				if mode == "429" {
					w.WriteHeader(429)
					_, _ = w.Write([]byte(recoveredQuotaBody))
					return
				}
				if strings.Contains(mode, "rearmed") {
					repo.mu.Lock()
					limited := time.Now()
					reset := time.Now().Add(time.Hour)
					repo.account.RateLimitedAt = &limited
					if mode == "rearmed" {
						repo.account.RateLimitResetAt = &reset
					}
					repo.mu.Unlock()
				}
				if mode == "missing data" {
					_, _ = w.Write([]byte("{}"))
					return
				}
				_, _ = w.Write([]byte(recoveredQuotaBody))
			}))
			defer srv.Close()
			svc := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, cache, nil), newQuotaRedirectingFactory(srv), nil)
			_, err := svc.QueryUsage(context.Background(), 123)
			if mode == "429" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			current, err := repo.GetByID(context.Background(), 123)
			require.NoError(t, err)
			require.Equal(t, mode == "recovered", current.RateLimitResetAt == nil)
			if mode == "write failed" || mode == "429" || mode == "missing data" {
				require.Zero(t, repo.clears)
			}
		})
	}
}

func TestOpenAIQuotaRecoveryProbeResponse(t *testing.T) {
	for _, tc := range []struct {
		name, primaryWindow, secondaryWindow, primaryUsed, secondaryUsed string
		status                                                           int
		want                                                             bool
	}{
		{"complete", "300", "10080", "0", "0", 200, true},
		{"swapped", "10080", "300", "0", "0", 200, true},
		{"missing windows", "", "", "0", "0", 200, false},
		{"missing primary window", "", "10080", "0", "0", 200, false},
		{"missing secondary window", "300", "", "0", "0", 200, false},
		{"duplicate short windows", "300", "300", "0", "0", 200, false},
		{"duplicate long windows", "10080", "10080", "0", "0", 200, false},
		{"unknown windows", "1", "2", "0", "0", 200, false},
		{"unknown primary window", "60", "10080", "0", "0", 200, false},
		{"unknown secondary window", "300", "1440", "0", "0", 200, false},
		{"invalid window", "bad", "10080", "0", "0", 200, false},
		{"missing primary usage", "300", "10080", "", "0", 200, false},
		{"missing secondary usage", "300", "10080", "0", "", 200, false},
		{"primary only", "300", "", "0", "", 200, false},
		{"secondary only", "", "10080", "", "0", 200, false},
		{"nonzero usage", "300", "10080", "0.1", "0", 200, false},
		{"invalid usage", "300", "10080", "bad", "0", 200, false},
		{"nan usage", "300", "10080", "NaN", "0", 200, false},
		{"infinite usage", "300", "10080", "0", "+Inf", 200, false},
		{"rate limited", "300", "10080", "0", "0", 429, false},
		{"unauthorized", "300", "10080", "0", "0", 401, false},
		{"unavailable", "300", "10080", "0", "0", 503, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &quotaRecoveryTestRepo{account: quotaRecoveryAccount()}
			svc := &AccountUsageService{accountRepo: repo}
			resp := &http.Response{StatusCode: tc.status, Header: make(http.Header)}
			resp.Header.Set("x-codex-primary-used-percent", tc.primaryUsed)
			resp.Header.Set("x-codex-primary-window-minutes", tc.primaryWindow)
			resp.Header.Set("x-codex-secondary-used-percent", tc.secondaryUsed)
			resp.Header.Set("x-codex-secondary-window-minutes", tc.secondaryWindow)
			updates, err := svc.persistOpenAICodexProbeResponse(context.Background(), &repo.account, resp, observeOpenAIQuotaRecovery(&repo.account, time.Now()))
			require.NoError(t, err)
			unlock := lockOpenAICodexSnapshotWrite(repo.account.ID)
			unlock()
			require.Equal(t, 1, repo.writes)
			require.NotEmpty(t, updates)
			require.Contains(t, repo.account.Extra, "codex_usage_updated_at")
			require.Equal(t, tc.want, repo.account.RateLimitResetAt == nil)
			require.Equal(t, tc.want, repo.clears == 1)
		})
	}
}

func TestOpenAIQuotaRecoveryUnblocksRuntimeScheduling(t *testing.T) {
	repo := &quotaRecoveryTestRepo{account: quotaRecoveryAccount()}
	gateway := &OpenAIGatewayService{}
	gateway.BlockAccountScheduling(&repo.account, *repo.account.RateLimitResetAt, "429")
	require.True(t, gateway.isOpenAIAccountRequestRuntimeBlocked(&repo.account, "gpt-5", false))
	recoverOpenAIQuotaRateLimit(context.Background(), repo, observeOpenAIQuotaRecovery(&repo.account, time.Now()), zeroQuotaUpdates())
	require.False(t, gateway.isOpenAIAccountRequestRuntimeBlocked(&repo.account, "gpt-5", false))
}
