//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestSharedQuotaResetRecoversBeforeRefreshAndReturnsOnlyQuota(t *testing.T) {
	for _, body := range []string{"", `{}`} {
		h, repo, quota, recovery := newSharedQuotaTestHandler()
		repo.account.Extra["codex_turn_ticket:model"] = "private-credit-id"
		c, w := sharedTestContext(7, body)
		h.ResetQuota(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, "success", gjson.GetBytes(w.Body.Bytes(), "data.code").String())
		require.Equal(t, int64(2), gjson.GetBytes(w.Body.Bytes(), "data.windows_reset").Int())
		require.True(t, gjson.GetBytes(w.Body.Bytes(), "data.cache_refreshed").Bool())
		require.True(t, gjson.GetBytes(w.Body.Bytes(), "data.account_state_recovered").Bool())
		require.Empty(t, gjson.GetBytes(w.Body.Bytes(), "data.warning_code").String())
		require.Equal(t, int64(3), gjson.GetBytes(w.Body.Bytes(), "data.quota.rate_limit_reset_credits.available_count").Int())
		require.Equal(t, int64(123), gjson.GetBytes(w.Body.Bytes(), "data.quota.fetched_at").Int())
		require.Equal(t, []string{"reset", "recover", "query", "cache"}, quota.sequence)
		require.Equal(t, 1, quota.resetCalls)
		require.Equal(t, 1, recovery.calls)
		require.True(t, recovery.options.InvalidateToken)
		require.Equal(t, int64(11), recovery.id)
		require.GreaterOrEqual(t, repo.reads, 2)
		assertSharedQuotaPrivateFieldsAbsent(t, w.Body.String())
	}
}

func TestSharedQuotaResetFailureStopsRecovery(t *testing.T) {
	for _, nilResult := range []bool{false, true} {
		h, _, quota, recovery := newSharedQuotaTestHandler()
		if nilResult {
			quota.reset = nil
		} else {
			quota.resetErr = errors.New("private-reset-error")
		}
		c, w := sharedTestContext(7, "")
		h.ResetQuota(c)
		require.GreaterOrEqual(t, w.Code, http.StatusInternalServerError)
		require.Equal(t, 1, quota.resetCalls)
		require.Zero(t, recovery.calls+quota.queryCalls+quota.postCalls)
		require.NotContains(t, w.Body.String(), "private-reset-error")
	}
}

func TestSharedQuotaResetSanitizesUpstreamCodeAndWindowCount(t *testing.T) {
	for _, tc := range []struct{ code, want string }{{"ok", "ok"}, {"private-arbitrary-upstream-text", "success"}} {
		h, _, quota, _ := newSharedQuotaTestHandler()
		quota.reset.Code, quota.reset.WindowsReset = tc.code, -1
		c, w := sharedTestContext(7, "")
		h.ResetQuota(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, tc.want, gjson.GetBytes(w.Body.Bytes(), "data.code").String())
		require.Zero(t, gjson.GetBytes(w.Body.Bytes(), "data.windows_reset").Int())
		require.NotContains(t, w.Body.String(), "private-arbitrary-upstream-text")
		assertSharedQuotaPrivateFieldsAbsent(t, w.Body.String())
	}
}

func TestSharedQuotaResetReportsPartialSuccess(t *testing.T) {
	for _, tc := range []struct {
		name, warning string
		recovered     bool
		queryCalls    int
		postCalls     int
	}{
		{"recovery", service.OpenAIQuotaResetWarningAccountRecoveryFailed, false, 0, 0},
		{"query", service.OpenAIQuotaResetWarningCacheRefreshFailed, true, 1, 0},
		{"empty query", service.OpenAIQuotaResetWarningCacheRefreshFailed, true, 1, 0},
		{"cache", service.OpenAIQuotaResetWarningCacheRefreshFailed, true, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, _, quota, recovery := newSharedQuotaTestHandler()
			switch tc.name {
			case "recovery":
				recovery.err = errors.New("private-recovery-error")
			case "query":
				quota.queryErr = errors.New("private-query-error")
			case "empty query":
				quota.usage = nil
			case "cache":
				quota.cacheErr = errors.New("private-cache-error")
			}
			c, w := sharedTestContext(7, "")
			h.ResetQuota(c)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, "success", gjson.GetBytes(w.Body.Bytes(), "data.code").String())
			require.Equal(t, tc.warning, gjson.GetBytes(w.Body.Bytes(), "data.warning_code").String())
			require.Equal(t, tc.recovered, gjson.GetBytes(w.Body.Bytes(), "data.account_state_recovered").Bool())
			require.False(t, gjson.GetBytes(w.Body.Bytes(), "data.cache_refreshed").Bool())
			require.Equal(t, tc.queryCalls, quota.queryCalls)
			require.Equal(t, tc.postCalls, quota.postCalls)
			require.Equal(t, 1, quota.resetCalls)
			require.Equal(t, 1, recovery.calls)
			require.NotContains(t, w.Body.String(), "private-")
			assertSharedQuotaPrivateFieldsAbsent(t, w.Body.String())
		})
	}
}

func TestSharedQuotaResetMissingRecoveryReportsPartialSuccess(t *testing.T) {
	h, _, quota, _ := newSharedQuotaTestHandler()
	h.recoverer = nil
	c, w := sharedTestContext(7, "")
	h.ResetQuota(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, service.OpenAIQuotaResetWarningAccountRecoveryFailed, gjson.GetBytes(w.Body.Bytes(), "data.warning_code").String())
	require.False(t, gjson.GetBytes(w.Body.Bytes(), "data.account_state_recovered").Bool())
	require.Zero(t, quota.queryCalls+quota.postCalls)
	require.Equal(t, 1, quota.resetCalls)
}

func TestSharedQuotaResetPostProcessingSurvivesClientCancellation(t *testing.T) {
	h, _, quota, recovery := newSharedQuotaTestHandler()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	quota.onReset = cancel
	c, w := sharedTestContext(7, "")
	c.Request = c.Request.WithContext(ctx)
	started := time.Now()
	h.ResetQuota(c)
	require.Equal(t, context.Canceled, ctx.Err())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, recovery.ctxErr)
	require.NoError(t, quota.queryCtxErr)
	require.NoError(t, quota.cacheCtxErr)
	require.True(t, recovery.deadline.After(started))
	require.WithinDuration(t, started.Add(8*time.Second), recovery.deadline, time.Second)
	require.True(t, gjson.GetBytes(w.Body.Bytes(), "data.account_state_recovered").Bool())
	require.True(t, gjson.GetBytes(w.Body.Bytes(), "data.cache_refreshed").Bool())
}

type sharedQuotaReloadRepository struct {
	*sharedTestRepository
	loadErr error
}

func (r *sharedQuotaReloadRepository) GetByID(ctx context.Context, id int64) (*service.Account, error) {
	if r.reads > 0 {
		return nil, r.loadErr
	}
	return r.sharedTestRepository.GetByID(ctx, id)
}

func TestSharedQuotaResetReloadFailureKeepsRecoveredState(t *testing.T) {
	h, repo, quota, recovery := newSharedQuotaTestHandler()
	reload := &sharedQuotaReloadRepository{sharedTestRepository: repo, loadErr: errors.New("private-reload-error")}
	h.pool = service.NewSharedPoolService(repo, reload, nil, nil, nil, nil, nil)
	c, w := sharedTestContext(7, "")
	h.ResetQuota(c)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.True(t, gjson.GetBytes(w.Body.Bytes(), "data.account_state_recovered").Bool())
	require.True(t, gjson.GetBytes(w.Body.Bytes(), "data.cache_refreshed").Bool())
	require.Equal(t, service.OpenAIQuotaResetWarningAccountRefreshFailed, gjson.GetBytes(w.Body.Bytes(), "data.warning_code").String())
	require.Equal(t, 1, recovery.calls)
	require.Equal(t, 1, quota.resetCalls)
	require.NotContains(t, w.Body.String(), "private-reload-error")
	assertSharedQuotaPrivateFieldsAbsent(t, w.Body.String())
}

func TestSharedQuotaResetConcurrentActionsConsumeOnlyOnce(t *testing.T) {
	h, _, quota, _ := newSharedQuotaTestHandler()
	entered, release := make(chan struct{}), make(chan struct{})
	var unblockOnce sync.Once
	unblock := func() { unblockOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	quota.onReset = func() { close(entered); <-release }
	finished := make(chan *httptest.ResponseRecorder, 1)
	c, first := sharedTestContext(7, "")
	go func() { h.ResetQuota(c); finished <- first }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("reset did not reach upstream")
	}
	for _, reset := range []bool{true, false} {
		c, w := sharedTestContext(7, "")
		if reset {
			h.ResetQuota(c)
		} else {
			h.RefreshQuota(c)
		}
		require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
	}
	require.Equal(t, 1, quota.resetCalls)
	require.Zero(t, quota.queryCalls)
	unblock()
	select {
	case w := <-finished:
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	case <-time.After(2 * time.Second):
		t.Fatal("reset did not complete after release")
	}
	c, limited := sharedTestContext(7, "")
	h.ResetQuota(c)
	require.Equal(t, http.StatusTooManyRequests, limited.Code)
	require.Equal(t, 1, quota.resetCalls)
	c, refreshed := sharedTestContext(7, "")
	h.RefreshQuota(c)
	require.Equal(t, http.StatusOK, refreshed.Code, refreshed.Body.String())
}
