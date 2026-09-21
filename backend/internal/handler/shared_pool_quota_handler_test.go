//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type sharedQuotaStub struct {
	usage       *service.OpenAIQuotaUsage
	reset       *service.OpenAIQuotaResetResult
	queryErr    error
	resetErr    error
	cacheErr    error
	queryCalls  int
	resetCalls  int
	cacheCalls  int
	postCalls   int
	queryCtxErr error
	cacheCtxErr error
	accountID   int64
	onReset     func()
	sequence    []string
}

func (s *sharedQuotaStub) QueryUsage(ctx context.Context, id int64) (*service.OpenAIQuotaUsage, error) {
	s.queryCalls++
	s.accountID, s.queryCtxErr = id, ctx.Err()
	s.sequence = append(s.sequence, "query")
	return s.usage, s.queryErr
}

func (s *sharedQuotaStub) ResetCredit(_ context.Context, id int64) (*service.OpenAIQuotaResetResult, error) {
	s.resetCalls++
	s.accountID = id
	s.sequence = append(s.sequence, "reset")
	if s.onReset != nil {
		s.onReset()
	}
	return s.reset, s.resetErr
}

func (s *sharedQuotaStub) CacheResetCreditsSnapshot(ctx context.Context, _ int64, _ *service.OpenAIRateLimitResetCredits) error {
	s.cacheCalls++
	s.cacheCtxErr = ctx.Err()
	return s.cacheErr
}

func (s *sharedQuotaStub) CachePostResetSnapshot(ctx context.Context, _ int64, _ *service.OpenAIQuotaUsage) error {
	s.postCalls++
	s.cacheCtxErr = ctx.Err()
	s.sequence = append(s.sequence, "cache")
	return s.cacheErr
}

type sharedQuotaRecoveryStub struct {
	err      error
	calls    int
	id       int64
	ctxErr   error
	deadline time.Time
	options  service.AccountRecoveryOptions
	quota    *sharedQuotaStub
}

func (s *sharedQuotaRecoveryStub) RecoverAccountState(ctx context.Context, id int64, options service.AccountRecoveryOptions) (*service.SuccessfulTestRecoveryResult, error) {
	s.calls++
	s.id, s.ctxErr, s.options = id, ctx.Err(), options
	s.deadline, _ = ctx.Deadline()
	s.quota.sequence = append(s.quota.sequence, "recover")
	return &service.SuccessfulTestRecoveryResult{}, s.err
}

func newSharedQuotaTestHandler() (*SharedPoolHandler, *sharedTestRepository, *sharedQuotaStub, *sharedQuotaRecoveryStub) {
	h, repo, _ := newSharedTestHandler()
	repo.account.Type = service.AccountTypeOAuth
	quota := &sharedQuotaStub{
		reset: &service.OpenAIQuotaResetResult{Code: "success", WindowsReset: 2,
			Credit: &service.OpenAIQuotaResetCredit{ID: "private-credit-id"}},
		usage: &service.OpenAIQuotaUsage{FetchedAt: 123, UserID: "private-user-id", AccountID: "private-account-id",
			Email: "private-email@example.invalid", PlanType: "pro",
			RateLimitResetCredits: &service.OpenAIRateLimitResetCredits{AvailableCount: 3,
				Credits: []service.OpenAIRateLimitResetCreditDetail{{ExpiresAt: "2027-01-01T00:00:00Z"}}}},
	}
	recovery := &sharedQuotaRecoveryStub{quota: quota}
	h.quota, h.recoverer = quota, recovery
	return h, repo, quota, recovery
}

func assertSharedQuotaPrivateFieldsAbsent(t *testing.T, body string) {
	t.Helper()
	for _, value := range []string{"private-credit-id", "private-user-id", "private-account-id", "private-email", "test-secret", `"credentials"`, `"account"`, `"credit"`} {
		require.NotContains(t, body, value)
	}
}

func TestSharedQuotaRequiresOwnedOpenAIOAuth(t *testing.T) {
	parentID := int64(2)
	for _, reset := range []bool{false, true} {
		for _, tc := range []struct {
			name, platform, accountType string
			owner                       int64
			parent                      *int64
			status                      int
		}{
			{"anonymous", service.PlatformOpenAI, service.AccountTypeOAuth, 0, nil, 401},
			{"other owner", service.PlatformOpenAI, service.AccountTypeOAuth, 8, nil, 404},
			{"API key", service.PlatformOpenAI, service.AccountTypeAPIKey, 7, nil, 400},
			{"Gemini", service.PlatformGemini, service.AccountTypeOAuth, 7, nil, 400},
			{"shadow", service.PlatformOpenAI, service.AccountTypeOAuth, 7, &parentID, 400},
		} {
			t.Run(tc.name+map[bool]string{false: "/refresh", true: "/reset"}[reset], func(t *testing.T) {
				h, repo, quota, recovery := newSharedQuotaTestHandler()
				repo.account.Platform, repo.account.Type, repo.account.ParentAccountID = tc.platform, tc.accountType, tc.parent
				c, w := sharedTestContext(tc.owner, "")
				if reset {
					h.ResetQuota(c)
				} else {
					h.RefreshQuota(c)
				}
				require.Equal(t, tc.status, w.Code, w.Body.String())
				require.Zero(t, quota.queryCalls+quota.resetCalls+quota.cacheCalls+quota.postCalls+recovery.calls)
				if tc.owner != 7 {
					require.Zero(t, repo.reads)
				}
			})
		}
	}
}

func TestSharedQuotaRejectsInvalidBodiesBeforeUpstream(t *testing.T) {
	for _, body := range []string{`{"credit_id":"selected"}`, `{"force":true}`, `[]`, `"text"`, `{`, `{} {}`} {
		for _, reset := range []bool{false, true} {
			h, _, quota, recovery := newSharedQuotaTestHandler()
			c, w := sharedTestContext(7, body)
			if reset {
				h.ResetQuota(c)
			} else {
				h.RefreshQuota(c)
			}
			require.Equal(t, http.StatusBadRequest, w.Code, "body=%s reset=%v response=%s", body, reset, w.Body.String())
			require.Zero(t, quota.queryCalls+quota.resetCalls+recovery.calls)
		}
	}
}

func TestSharedQuotaRefreshReturnsSanitizedSnapshotAndPersistenceStatus(t *testing.T) {
	for _, persistFailed := range []bool{false, true} {
		for _, body := range []string{"", `{}`} {
			h, _, quota, recovery := newSharedQuotaTestHandler()
			if persistFailed {
				quota.cacheErr = errors.New("private-cache-error")
			}
			c, w := sharedTestContext(7, body)
			h.RefreshQuota(c)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, int64(3), gjson.GetBytes(w.Body.Bytes(), "data.rate_limit_reset_credits.available_count").Int())
			require.Equal(t, int64(123), gjson.GetBytes(w.Body.Bytes(), "data.fetched_at").Int())
			require.Equal(t, !persistFailed, gjson.GetBytes(w.Body.Bytes(), "data.cache_persisted").Bool())
			require.Equal(t, 1, quota.queryCalls)
			require.Equal(t, 1, quota.cacheCalls)
			require.Equal(t, int64(11), quota.accountID)
			require.Zero(t, quota.resetCalls+quota.postCalls+recovery.calls)
			require.NotContains(t, w.Body.String(), "private-cache-error")
			assertSharedQuotaPrivateFieldsAbsent(t, w.Body.String())
		}
	}
}

func TestSharedQuotaRefreshFailureDoesNotPersist(t *testing.T) {
	for _, nilResult := range []bool{false, true} {
		h, _, quota, _ := newSharedQuotaTestHandler()
		if nilResult {
			quota.usage = nil
		} else {
			quota.queryErr = errors.New("private-upstream-error")
		}
		c, w := sharedTestContext(7, "")
		h.RefreshQuota(c)
		require.GreaterOrEqual(t, w.Code, http.StatusInternalServerError)
		require.Zero(t, quota.cacheCalls+quota.resetCalls+quota.postCalls)
		require.NotContains(t, w.Body.String(), "private-upstream-error")
	}
}

func TestSharedQuotaRejectsInvalidIDAndUnavailableService(t *testing.T) {
	for _, reset := range []bool{false, true} {
		for _, invalidID := range []bool{false, true} {
			h, _, quota, recovery := newSharedQuotaTestHandler()
			c, w := sharedTestContext(7, "")
			want := http.StatusServiceUnavailable
			if invalidID {
				c.Params[0].Value = "invalid"
				want = http.StatusBadRequest
			} else {
				h.quota = nil
			}
			if reset {
				h.ResetQuota(c)
			} else {
				h.RefreshQuota(c)
			}
			require.Equal(t, want, w.Code, w.Body.String())
			require.Zero(t, quota.queryCalls+quota.resetCalls+recovery.calls)
		}
	}
}
