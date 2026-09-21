//go:build unit

package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type sharedTicketSettingsStub struct {
	enabled bool
	calls   int
}

func (s *sharedTicketSettingsStub) GetOpenAICodexTicketEnabled(ctx context.Context, fallback bool) bool {
	s.calls++
	if ctx.Err() != nil {
		return fallback
	}
	return s.enabled
}

func TestSharedUsageTicketSummaryRespectsLiveGlobalAndAccountSwitch(t *testing.T) {
	h, repo, _ := newSharedTestHandler()
	repo.account.Type = service.AccountTypeOAuth
	h.usage = &sharedUsageStub{result: &service.UsageInfo{}}
	h.ticketConfig = &config.Config{}
	h.ticketConfig.Gateway.OpenAICodexTicket = config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"gpt-6-astra"}, FailClosed: true}
	settings := &sharedTicketSettingsStub{enabled: false}
	h.ticketSettings = settings
	state := "gAAAAA" + strings.Repeat("B", 286)
	repo.account.Extra["codex_turn_ticket:gpt-6-astra"] = map[string]any{
		"model": "gpt-6-astra", "state": state, "length": 292, "expires_at": time.Now().Add(time.Hour),
	}
	for _, tc := range []struct{ global, account, ready bool }{{false, true, false}, {true, false, false}, {true, true, true}} {
		settings.enabled = tc.global
		repo.account.Extra[service.OpenAICodexTicketEnabledExtraKey] = tc.account
		c, w := sharedUsageContext(7, "")
		h.GetUsage(c)
		require.Equal(t, http.StatusOK, w.Code)
		tickets := gjson.GetBytes(w.Body.Bytes(), "data.codex_turn_tickets")
		require.Equal(t, tc.ready, tickets.Exists())
		if tc.ready {
			require.Equal(t, int64(292), tickets.Get("0.length").Int())
			require.True(t, tickets.Get("0.ready").Bool())
			require.False(t, tickets.Get("0.blocked").Bool())
		}
		require.NotContains(t, w.Body.String(), state)
		require.NotContains(t, w.Body.String(), "test-secret")
	}
	require.Equal(t, 3, settings.calls)
	settings.enabled = false
	repo.account.Extra[service.OpenAICodexTicketEnabledExtraKey] = true
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	view := sharedAccountUsageView{}
	h.enrichSharedUsageView(ctx, &view, repo.account)
	require.Empty(t, view.CodexTurnTickets, "不能因客户端取消而退回已被后台关闭的配置开关")
}

func TestSharedUsageResetSnapshotOnlyContainsCountAndValidExpirations(t *testing.T) {
	h, repo, _ := newSharedTestHandler()
	repo.account.Type = service.AccountTypeOAuth
	h.usage = &sharedUsageStub{result: &service.UsageInfo{}}
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	repo.account.Extra["codex_reset_credit_snapshot"] = map[string]any{
		"available_count": 2, "account_id": "private-id", "email": "private-email",
		"credits": []map[string]any{{"id": "private-credit", "expires_at": expires}, {"expires_at": "private-invalid-time"}},
	}
	c, w := sharedUsageContext(7, "")
	h.GetUsage(c)
	require.Equal(t, http.StatusOK, w.Code)
	snapshot := gjson.GetBytes(w.Body.Bytes(), "data.codex_reset_credit_snapshot")
	require.Equal(t, int64(2), snapshot.Get("available_count").Int())
	require.Equal(t, expires, snapshot.Get("credits.0.expires_at").String())
	require.NotContains(t, w.Body.String(), "private")
	parent := int64(99)
	repo.account.ParentAccountID = &parent
	c, w = sharedUsageContext(7, "")
	h.GetUsage(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.False(t, gjson.GetBytes(w.Body.Bytes(), "data.codex_reset_credit_snapshot").Exists())
}

func TestSharedUsageForceCooldownStillAllowsNormalCachedLoad(t *testing.T) {
	h, repo, _ := newSharedTestHandler()
	repo.account.Type = service.AccountTypeOAuth
	usage := &sharedUsageStub{result: &service.UsageInfo{}}
	h.usage = usage
	c, w := sharedUsageContext(7, "?force=true")
	h.GetUsage(c)
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, usage.forced)
	c, w = sharedUsageContext(7, "?force=true")
	h.GetUsage(c)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.NotEmpty(t, w.Header().Get("Retry-After"))
	require.Equal(t, 1, usage.active)
	for _, query := range []string{"", "?force=false"} {
		c, w = sharedUsageContext(7, query)
		h.GetUsage(c)
		require.Equal(t, http.StatusOK, w.Code)
		require.False(t, usage.forced)
	}
	require.Equal(t, 3, usage.active)
}

type sharedBlockingUsage struct {
	sharedUsageStub
	started, release chan struct{}
}

func (s *sharedBlockingUsage) GetUsageForAccount(context.Context, *service.Account, ...bool) (*service.UsageInfo, error) {
	close(s.started)
	<-s.release
	return &service.UsageInfo{}, nil
}

func TestSharedUsageForcedRefreshRejectsConcurrentProbe(t *testing.T) {
	h, repo, _ := newSharedTestHandler()
	repo.account.Type = service.AccountTypeOAuth
	usage := &sharedBlockingUsage{started: make(chan struct{}), release: make(chan struct{})}
	h.usage = usage
	first, recorder := sharedUsageContext(7, "?force=true")
	done := make(chan struct{})
	go func() { h.GetUsage(first); close(done) }()
	select {
	case <-usage.started:
	case <-time.After(time.Second):
		t.Fatal("first probe did not start")
	}
	for _, query := range []string{"?force=true", ""} {
		second, duplicate := sharedUsageContext(7, query)
		h.GetUsage(second)
		require.Equal(t, http.StatusTooManyRequests, duplicate.Code)
	}
	close(usage.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("first probe did not finish")
	}
	require.Equal(t, http.StatusOK, recorder.Code)
}

func TestSharedUsageExpiredSnapshotLoadSerializesQuotaReset(t *testing.T) {
	h, repo, quota, _ := newSharedQuotaTestHandler()
	repo.account.Extra["codex_usage_updated_at"] = time.Now().Add(-time.Hour).Format(time.RFC3339)
	usage := &sharedBlockingUsage{started: make(chan struct{}), release: make(chan struct{})}
	h.usage = usage
	first, recorder := sharedUsageContext(7, "")
	done := make(chan struct{})
	go func() { h.GetUsage(first); close(done) }()
	select {
	case <-usage.started:
	case <-time.After(time.Second):
		t.Fatal("normal load did not start its probe")
	}
	reset, response := sharedTestContext(7, "")
	h.ResetQuota(reset)
	require.Equal(t, http.StatusTooManyRequests, response.Code)
	require.Zero(t, quota.resetCalls)
	close(usage.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("normal load did not finish")
	}
	require.Equal(t, http.StatusOK, recorder.Code)
	reset, response = sharedTestContext(7, "")
	h.ResetQuota(reset)
	require.Equal(t, http.StatusOK, response.Code)
	require.Equal(t, 1, quota.resetCalls)
}
