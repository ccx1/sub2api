package service

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type routeAffinityDialer struct {
	mu        sync.Mutex
	headers   []http.Header
	responses []http.Header
}

func (d *routeAffinityDialer) Dial(_ context.Context, _ string, h http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	index := len(d.headers)
	d.headers = append(d.headers, h.Clone())
	var response http.Header
	if index < len(d.responses) {
		response = d.responses[index].Clone()
	}
	return &openAIWSFakeConn{}, http.StatusSwitchingProtocols, response, nil
}

func (d *routeAffinityDialer) sentHeaders() []http.Header {
	d.mu.Lock()
	defer d.mu.Unlock()
	result := make([]http.Header, len(d.headers))
	for i, h := range d.headers {
		result[i] = h.Clone()
	}
	return result
}

func routeAffinityFixture(t *testing.T) (*openAIWSConnPool, openAIWSAcquireRequest, *routeAffinityDialer) {
	t.Helper()
	s, account, req := codexCookieWSFixtureForPolicy(t, "cookie", false)
	ticket := req.CodexTicketReceipt.ticket
	ticket.Cookies = []*http.Cookie{{Name: "__oailb", Value: "route-a", Path: "/backend-api", Secure: true, Expires: ticket.ExpiresAt}}
	req.CodexTicketReceipt = &openAICodexTicketWSReceipt{account: account, ticket: ticket, config: req.CodexTicketReceipt.config}
	ticket.applyHeaders(req.Headers)
	req.Headers.Set("x-codex-routing-hint", "model="+ticket.Model+";tier=default")
	req.RouteAffinityMode = CodexRouteAffinityStrict
	s.cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 8
	s.cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 8
	s.cfg.Gateway.OpenAIWS.MinIdlePerAccount = 0
	pool := newOpenAIWSConnPool(s.cfg)
	t.Cleanup(pool.Close)
	dialer := &routeAffinityDialer{}
	pool.setClientDialerForTest(dialer)
	return pool, req, dialer
}

func TestOpenAIWSRouteAffinityStrictRejectsUntrustedOrExpiredRequest(t *testing.T) {
	tests := []struct {
		name   string
		change func(*openAIWSAcquireRequest)
	}{
		{"unmanaged_cookie", func(r *openAIWSAcquireRequest) { r.CodexTicketReceipt = nil }},
		{"unverified", func(r *openAIWSAcquireRequest) { r.CodexTicketReceipt.ticket.Verified = false }},
		{"verification_skipped", func(r *openAIWSAcquireRequest) { r.CodexTicketReceipt.ticket.VerificationSkipped = true }},
		{"wrong_model", func(r *openAIWSAcquireRequest) {
			r.Headers.Set("x-codex-routing-hint", "model=other-model;tier=default")
		}},
		{"model_not_gated", func(r *openAIWSAcquireRequest) { r.CodexTicketReceipt.config.Models = []string{"other-model"} }},
		{"expired_fail_open", func(r *openAIWSAcquireRequest) { r.CodexTicketReceipt.ticket.ExpiresAt = time.Now().Add(-time.Second) }},
		{"quality_blocked", func(r *openAIWSAcquireRequest) { r.RouteQualityBlocked = true }},
		{"no_route_marker", func(r *openAIWSAcquireRequest) {
			r.CodexTicketReceipt.ticket.Cookies[0].Name = "ticket_session"
			r.CodexTicketReceipt.ticket.applyHeaders(r.Headers)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool, req, dialer := routeAffinityFixture(t)
			tt.change(&req)
			lease, err := pool.Acquire(context.Background(), req)
			require.ErrorIs(t, err, errOpenAIWSRouteAffinityUnavailable)
			require.Nil(t, lease)
			require.Empty(t, dialer.sentHeaders(), "拒绝必须发生在出站握手之前")
		})
	}
}

func TestOpenAIWSRouteAffinityStrictReusesSentCookieWithoutSetCookie(t *testing.T) {
	pool, req, dialer := routeAffinityFixture(t)
	dialer.responses = []http.Header{{"Server": {"cloudflare"}}}
	first, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	first.Release()
	second, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	defer second.Release()
	require.Equal(t, first.ConnID(), second.ConnID())
	require.True(t, second.Reused())
	require.Equal(t, []http.Header{req.Headers}, dialer.sentHeaders())
	model := req.CodexTicketReceipt.ticket.Model
	require.NoError(t, second.WriteJSON(map[string]any{"type": "response.create", "model": model}, 0))
	require.ErrorIs(t, second.WriteJSON(map[string]any{"type": "response.create", "model": "other-model"}, 0), errOpenAIWSRouteAffinityUnavailable)
}

func TestOpenAIWSRouteAffinityHandshakeChangeDoesNotReplaceOriginal(t *testing.T) {
	for _, cookie := range []string{"__oailb=route-b; Path=/backend-api", "__oailb=; Path=/backend-api; Max-Age=-1"} {
		t.Run(cookie, func(t *testing.T) {
			pool, req, dialer := routeAffinityFixture(t)
			dialer.responses = []http.Header{nil, {"Set-Cookie": {cookie}}}
			first, err := pool.Acquire(context.Background(), req)
			require.NoError(t, err)
			first.Release()
			req.ForceNewConn = true
			rejected, err := pool.Acquire(context.Background(), req)
			require.ErrorIs(t, err, errOpenAIWSRouteAffinityUnavailable)
			require.Nil(t, rejected)
			req.ForceNewConn = false
			again, err := pool.Acquire(context.Background(), req)
			require.NoError(t, err)
			defer again.Release()
			require.Equal(t, first.ConnID(), again.ConnID(), "响应中的新路由不得替换原票据亲和")
			require.Equal(t, []http.Header{req.Headers, req.Headers}, dialer.sentHeaders())
		})
	}
}

func TestOpenAIWSRouteAffinityWriteRejectsExpiredFailOpenReceipt(t *testing.T) {
	for _, initialMode := range []string{CodexRouteAffinityOff, CodexRouteAffinityStrict} {
		t.Run(initialMode, func(t *testing.T) {
			pool, req, _ := routeAffinityFixture(t)
			initial := req
			initial.RouteAffinityMode = initialMode
			first, err := pool.Acquire(context.Background(), initial)
			require.NoError(t, err)
			first.Release()
			lease, err := pool.Acquire(context.Background(), req)
			require.NoError(t, err)
			defer lease.Release()
			// 模拟已有连接跨越硬过期边界；fail-open 不能覆盖 strict 的限制。
			req.CodexTicketReceipt.ticket.ExpiresAt = time.Now().Add(-time.Second)
			err = lease.WriteJSON(map[string]any{"type": "response.create", "model": req.CodexTicketReceipt.ticket.Model}, 0)
			require.ErrorIs(t, err, errOpenAIWSRouteAffinityUnavailable)
			inner := lease.conn.ws.(*openAIWSFakeConn)
			inner.mu.Lock()
			defer inner.mu.Unlock()
			require.Empty(t, inner.payload, "过期后不能发送新的业务请求")
		})
	}
}

func TestOpenAIWSRouteAffinityPreferredConnCannotBypassStrict(t *testing.T) {
	pool, req, dialer := routeAffinityFixture(t)
	dialer.responses = []http.Header{{"Set-Cookie": {"__oailb=route-b; Path=/backend-api"}}}
	off := req
	off.RouteAffinityMode = CodexRouteAffinityOff
	wrong, err := pool.Acquire(context.Background(), off)
	require.NoError(t, err)
	wrong.Release()
	req.PreferredConnID, req.ForcePreferredConn = wrong.ConnID(), true
	rejected, err := pool.Acquire(context.Background(), req)
	require.ErrorIs(t, err, errOpenAIWSPreferredConnUnavailable)
	require.Nil(t, rejected)
	req.ForcePreferredConn = false
	valid, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	defer valid.Release()
	require.NotEqual(t, wrong.ConnID(), valid.ConnID())
	require.Len(t, dialer.sentHeaders(), 2)
}

func TestOpenAIWSRouteAffinityForceNewConsumesOnlyUnusedMatchingConn(t *testing.T) {
	pool, req, dialer := routeAffinityFixture(t)
	dialer.responses = []http.Header{nil, {"Set-Cookie": {"__oailb=route-b; Path=/backend-api"}}}
	used, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	used.Release()
	off := req
	off.RouteAffinityMode = CodexRouteAffinityOff
	wrong, err := pool.dialConn(context.Background(), off)
	require.NoError(t, err)
	ap := pool.getOrCreateAccountPool(req.Account.ID)
	ap.mu.Lock()
	ap.conns[wrong.id] = wrong
	ap.mu.Unlock()
	pool.prewarmConns(req.Account.ID, req, 1)
	req.ForceNewConn = true
	fresh, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	require.NotEqual(t, used.ConnID(), fresh.ConnID())
	require.NotEqual(t, wrong.id, fresh.ConnID())
	require.Len(t, dialer.sentHeaders(), 3, "应消费尚未使用的预热连接，无需再次握手")
	fresh.Release()
	next, err := pool.Acquire(context.Background(), req)
	require.NoError(t, err)
	defer next.Release()
	require.NotEqual(t, fresh.ConnID(), next.ConnID(), "用过的预热连接不能再次充当新连接")
	require.NotEqual(t, wrong.id, next.ConnID())
	require.Len(t, dialer.sentHeaders(), 4)
}

func TestOpenAIWSRouteAffinityPrewarmHonorsCountAndCredentialSnapshot(t *testing.T) {
	for _, count := range []int{0, 2} {
		t.Run(string(rune('0'+count)), func(t *testing.T) {
			pool, req, dialer := routeAffinityFixture(t)
			pool.cfg.Gateway.OpenAIWS.MinIdlePerAccount = 4
			req.RoutePrewarmConnections, req.ForceNewConn = count, true
			ap := pool.getOrCreateAccountPool(req.Account.ID)
			ap.mu.Lock()
			ap.lastAcquire = cloneOpenAIWSAcquireRequestPtr(&req)
			ap.mu.Unlock()
			pool.ensureTargetIdleAsync(req.Account.ID)
			require.Eventually(t, func() bool {
				ap.mu.Lock()
				defer ap.mu.Unlock()
				return !ap.prewarmActive && len(ap.conns) == count
			}, time.Second, time.Millisecond)
			headers := dialer.sentHeaders()
			require.Len(t, headers, count)
			for _, sent := range headers {
				require.Equal(t, req.Headers, sent, "预热必须沿用同一票据、模型和出口身份头")
			}
		})
	}
}
