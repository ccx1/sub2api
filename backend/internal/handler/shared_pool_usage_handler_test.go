//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type sharedUsageStub struct {
	result  *service.UsageInfo
	err     error
	active  int
	passive int
	account *service.Account
	forced  bool
}

func (s *sharedUsageStub) GetUsageForAccount(ctx context.Context, account *service.Account, force ...bool) (*service.UsageInfo, error) {
	s.active++
	s.account = account
	s.forced = len(force) > 0 && force[0]
	return s.result, s.err
}

func (s *sharedUsageStub) GetPassiveUsage(ctx context.Context, id int64) (*service.UsageInfo, error) {
	s.passive++
	return s.result, s.err
}

func sharedUsageContext(owner int64, query string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/shared-pool/accounts/11/usage"+query, nil)
	c.Params = gin.Params{{Key: "id", Value: "11"}}
	if owner > 0 {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: owner})
	}
	return c, w
}

func TestSharedUsageRequiresOwnedOAuthAccount(t *testing.T) {
	for _, tc := range []struct {
		name     string
		owner    int64
		typeName string
		status   int
	}{
		{"anonymous", 0, service.AccountTypeOAuth, 401},
		{"other owner", 8, service.AccountTypeOAuth, 404},
		{"API key", 7, service.AccountTypeAPIKey, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, repo, _ := newSharedTestHandler()
			repo.account.Type = tc.typeName
			usage := &sharedUsageStub{result: &service.UsageInfo{}}
			h.usage = usage
			c, w := sharedUsageContext(tc.owner, "")
			h.GetUsage(c)
			require.Equal(t, tc.status, w.Code)
			require.Zero(t, usage.active+usage.passive)
			if tc.owner != 7 {
				require.Zero(t, repo.reads)
			}
		})
	}
}

func TestSharedUsageKeepsDisabledAccountVisibleAndDoesNotForceProbe(t *testing.T) {
	h, repo, _ := newSharedTestHandler()
	repo.account.Type = service.AccountTypeOAuth
	repo.disabled = true
	usage := &sharedUsageStub{result: &service.UsageInfo{}}
	h.usage = usage
	c, w := sharedUsageContext(7, "")
	h.GetUsage(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, 1, usage.active)
	require.Equal(t, int64(11), usage.account.ID)
	require.False(t, usage.forced)
	require.Equal(t, "active", gjson.GetBytes(w.Body.Bytes(), "data.source").String())
}

func TestSharedUsageValidatesSourceAndForce(t *testing.T) {
	for _, query := range []string{"?source=invalid", "?source=passive", "?force=invalid", "?force=true&force=false"} {
		h, repo, _ := newSharedTestHandler()
		repo.account.Type = service.AccountTypeOAuth
		usage := &sharedUsageStub{}
		h.usage = usage
		c, w := sharedUsageContext(7, query)
		h.GetUsage(c)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Zero(t, usage.active+usage.passive)
	}
}

func TestSharedUsageRejectsInvalidIDAndMissingService(t *testing.T) {
	h, repo, _ := newSharedTestHandler()
	repo.account.Type = service.AccountTypeOAuth
	c, w := sharedUsageContext(7, "")
	c.Params[0].Value = "invalid"
	h.GetUsage(c)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Zero(t, repo.reads)
	c, w = sharedUsageContext(7, "")
	h.GetUsage(c)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestSharedUsagePreservesPlatformWindows(t *testing.T) {
	reset := time.Now().Add(time.Hour).UTC()
	window := &service.UsageProgress{Utilization: 0, ResetsAt: &reset, RemainingSeconds: 3600,
		WindowStats: &service.WindowStats{Requests: 2, Tokens: 500, Cost: 1.25}}
	for _, tc := range []struct {
		platform string
		query    string
		usage    service.UsageInfo
		paths    []string
	}{
		{service.PlatformOpenAI, "", service.UsageInfo{FiveHour: window, SevenDay: window}, []string{"five_hour", "seven_day"}},
		{service.PlatformAnthropic, "?source=passive", service.UsageInfo{FiveHour: window, SevenDay: window, SevenDaySonnet: window, SevenDayFable: window}, []string{"five_hour", "seven_day", "seven_day_sonnet", "seven_day_fable"}},
		{service.PlatformGemini, "", service.UsageInfo{GeminiSharedDaily: window, GeminiProDaily: window, GeminiFlashDaily: window, GeminiSharedMinute: window, GeminiProMinute: window, GeminiFlashMinute: window}, []string{"gemini_shared_daily", "gemini_pro_daily", "gemini_flash_daily", "gemini_shared_minute", "gemini_pro_minute", "gemini_flash_minute"}},
		{service.PlatformAntigravity, "", service.UsageInfo{AntigravityQuota: map[string]*service.AntigravityModelQuota{"claude-sonnet": {Utilization: 0, ResetTime: reset.Format(time.RFC3339)}}}, nil},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			h, repo, _ := newSharedTestHandler()
			repo.account.Type, repo.account.Platform = service.AccountTypeOAuth, tc.platform
			tc.usage.UpdatedAt = &reset
			usage := &sharedUsageStub{result: &tc.usage}
			h.usage = usage
			c, w := sharedUsageContext(7, tc.query)
			h.GetUsage(c)
			require.Equal(t, http.StatusOK, w.Code)
			require.True(t, gjson.GetBytes(w.Body.Bytes(), "data.updated_at").Exists())
			for _, path := range tc.paths {
				value := gjson.GetBytes(w.Body.Bytes(), "data."+path)
				require.True(t, value.Exists(), path)
				require.Equal(t, 0.0, value.Get("utilization").Float())
				require.Equal(t, 1.25, value.Get("window_stats.cost").Float())
			}
			if tc.platform == service.PlatformAntigravity {
				require.True(t, gjson.GetBytes(w.Body.Bytes(), "data.antigravity_quota.claude-sonnet.reset_time").Exists())
			}
			if tc.query != "" {
				require.Equal(t, 1, usage.passive)
				require.Zero(t, usage.active)
			}
		})
	}
}

func TestSharedUsageRedactsUpstreamDetailsWithoutMutatingCachedResult(t *testing.T) {
	h, repo, _ := newSharedTestHandler()
	repo.account.Type = service.AccountTypeOAuth
	upstream := &service.UsageInfo{Error: "proxy://user:secret@internal", ErrorCode: "network_error",
		ForbiddenReason: "Bearer secret", ValidationURL: "https://verify.example/secret", IsForbidden: true, NeedsVerify: true,
		ModelForwardingRules: map[string]string{"private": "secret"}, SubscriptionTierRaw: "secret"}
	h.usage = &sharedUsageStub{result: upstream}
	c, w := sharedUsageContext(7, "")
	h.GetUsage(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.NotContains(t, w.Body.String(), "secret")
	require.NotContains(t, w.Body.String(), "validation_url")
	require.NotContains(t, w.Body.String(), "forbidden_reason")
	require.True(t, gjson.GetBytes(w.Body.Bytes(), "data.needs_verify").Bool())
	require.Equal(t, "network_error", gjson.GetBytes(w.Body.Bytes(), "data.error_code").String())
	require.Equal(t, "proxy://user:secret@internal", upstream.Error)
	for _, result := range []*service.UsageInfo{nil, {ErrorCode: "secret"}} {
		h.usage = &sharedUsageStub{result: result, err: errors.New("Bearer secret")}
		c, w = sharedUsageContext(7, "")
		h.GetUsage(c)
		require.Equal(t, http.StatusBadGateway, w.Code)
		require.NotContains(t, w.Body.String(), "secret")
	}
}

func TestSharedUsageStatusHasSafeMessage(t *testing.T) {
	for _, usage := range []service.UsageInfo{
		{IsForbidden: true}, {NeedsVerify: true}, {IsBanned: true}, {NeedsReauth: true},
		{ErrorCode: "Bearer secret", Source: "secret"},
	} {
		view := sharedUsageView(&usage, "active")
		require.NotEmpty(t, view.Error)
		require.Equal(t, "active", view.Source)
		require.Empty(t, view.ErrorCode)
	}
}

type sharedUsageStatsRepo struct{ service.UsageLogRepository }

func (r *sharedUsageStatsRepo) GetAccountWindowStats(context.Context, int64, time.Time) (*usagestats.AccountStats, error) {
	return &usagestats.AccountStats{Requests: 3, Cost: 2}, nil
}

func TestSharedUsageUsesExistingServiceSnapshots(t *testing.T) {
	for _, platform := range []string{service.PlatformOpenAI, service.PlatformAnthropic} {
		t.Run(platform, func(t *testing.T) {
			h, repo, _ := newSharedTestHandler()
			repo.account.Type, repo.account.Platform = service.AccountTypeOAuth, platform
			reset := time.Now().Add(time.Hour)
			repo.account.Extra = map[string]any{"codex_5h_used_percent": 25, "codex_5h_reset_at": reset.Format(time.RFC3339),
				"codex_7d_used_percent": 40, "codex_7d_reset_at": reset.Format(time.RFC3339), "codex_usage_updated_at": time.Now().Format(time.RFC3339),
				"passive_usage_7d_utilization": 0.4, "passive_usage_7d_reset": reset.Unix()}
			h.usage = service.NewAccountUsageService(repo, &sharedUsageStatsRepo{}, nil, nil, nil, nil, nil, nil, service.NewUsageCache(), nil, nil)
			query := ""
			if platform == service.PlatformAnthropic {
				query = "?source=passive"
			}
			c, w := sharedUsageContext(7, query)
			h.GetUsage(c)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, 40.0, gjson.GetBytes(w.Body.Bytes(), "data.seven_day.utilization").Float())
			require.NotContains(t, w.Body.String(), "credentials")
		})
	}
}
