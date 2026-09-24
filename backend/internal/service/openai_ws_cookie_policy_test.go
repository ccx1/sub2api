package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIWSTicketCompatibilityIncludesCookieMode(t *testing.T) {
	base := openAIWSAcquireRequest{CookieMode: CodexCookiePreserve}
	stripped := base
	stripped.CookieMode = CodexCookieStripInfrastructure
	require.NotEqual(t,
		normalizeOpenAIWSTicketCompatibility(base),
		normalizeOpenAIWSTicketCompatibility(stripped),
		"不同 Cookie 投影不能复用同一 WebSocket 握手")
}

func TestCodexTicketWSCookieTransitionRejectsModeChange(t *testing.T) {
	current := &openAICodexTicketWSReceipt{ticket: openAICodexTicket{
		CredentialMode: "cookie", CookieMode: CodexCookiePreserve,
	}}
	next := &openAICodexTicketWSReceipt{ticket: openAICodexTicket{
		CredentialMode: "cookie", CookieMode: CodexCookieStripCloudflare,
	}}
	require.False(t, codexTicketWSCookieTransition(current, next))
	require.True(t, codexTicketWSCookieTransition(current, nil), "默认模式保留显式 fail-open 续链")
}

type cookiePolicyHandshakeDialer struct {
	status int
	cookie string
}

func (d *cookiePolicyHandshakeDialer) Dial(_ context.Context, _ string, _ http.Header, _ string) (openAIWSClientConn, int, http.Header, error) {
	headers := http.Header{"Set-Cookie": {d.cookie}}
	if d.status != http.StatusSwitchingProtocols {
		return nil, d.status, headers, errors.New("test handshake rejected")
	}
	return &openAIWSFakeConn{}, d.status, headers, nil
}

func TestOpenAIWSCookiePolicyCapturesEveryDialResponse(t *testing.T) {
	for _, status := range []int{http.StatusSwitchingProtocols, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s, account, req := codexCookieWSFixture(t)
			pool := newOpenAIWSConnPool(s.cfg)
			t.Cleanup(pool.Close)
			dialer := &cookiePolicyHandshakeDialer{status: status,
				cookie: "__cf_bm=first; Path=/backend-api; Secure; Max-Age=300"}
			pool.setClientDialerForTest(dialer)
			for _, value := range []string{"first", "second"} {
				dialer.cookie = "__cf_bm=" + value + "; Path=/backend-api; Secure; Max-Age=300"
				conn, err := pool.dialConn(context.Background(), req)
				if status == http.StatusSwitchingProtocols {
					require.NoError(t, err)
					conn.close()
				} else {
					require.Error(t, err)
				}
				pending := s.pendingCodexTicketCookies(account.ID, req.CodexTicketReceipt.ticket.Model)
				require.NotNil(t, pending, "升级失败和未被借用的握手也必须采集响应 Cookie")
				require.Contains(t, codexTicketCookieHeaderValues(pending.Candidate.Cookies), "__cf_bm="+value)
			}
			require.Len(t, req.CodexTicketReceipt.ticket.Cookies, 1, "握手不能改变原始票据快照")
		})
	}
}

func TestOpenAIWSCookiePolicyPrewarmCapturesResponse(t *testing.T) {
	s, account, req := codexCookieWSFixture(t)
	s.cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 2
	pool := newOpenAIWSConnPool(s.cfg)
	t.Cleanup(pool.Close)
	pool.setClientDialerForTest(&cookiePolicyHandshakeDialer{status: http.StatusSwitchingProtocols,
		cookie: "__oailb=prewarmed; Path=/backend-api; Secure; Max-Age=300"})
	ap := pool.getOrCreateAccountPool(account.ID)
	ap.lastAcquire = cloneOpenAIWSAcquireRequestPtr(&req)
	ap.creating = 1
	pool.prewarmConns(account.ID, req, 1)
	pending := s.pendingCodexTicketCookies(account.ID, req.CodexTicketReceipt.ticket.Model)
	require.NotNil(t, pending)
	require.Contains(t, codexTicketCookieHeaderValues(pending.Candidate.Cookies), "__oailb=prewarmed")
	for _, conn := range ap.conns {
		require.False(t, conn.leasedBefore.Load(), "验证未向业务借出的预热连接")
	}
}

func TestOpenAIWSCookiePolicyExpiredProjectionDoesNotFallback(t *testing.T) {
	_, _, req := codexCookieWSFixtureForPolicy(t, "cookie", false)
	req.CodexTicketReceipt.ticket.CookieMode = CodexCookieStripInfrastructure
	req.CodexTicketReceipt.ticket.CookiePolicyVerified = true
	req.CodexTicketReceipt.ticket.ExpiresAt = time.Now().Add(-time.Second)
	discardExpiredOpenAICodexTicketWSHandshake(&req, time.Now())
	require.NotNil(t, req.CodexTicketReceipt)
	require.ErrorIs(t, req.CodexTicketReceipt.validate(time.Now()), ErrOpenAICodexTicketUnavailable)
}

func TestOpenAIWSCookiePolicyFiltersHeadersFactoryWithoutTicket(t *testing.T) {
	s, _, req := codexCookieWSFixture(t)
	req.CodexTicketReceipt = nil
	req.CookieMode = CodexCookieStripInfrastructure
	req.HeadersFactory = func(_ context.Context, headers http.Header) (http.Header, error) {
		headers.Set("Cookie", "session=account; __oailb=old; cf_clearance=old")
		return headers, nil
	}
	pool := newOpenAIWSConnPool(s.cfg)
	t.Cleanup(pool.Close)
	dialer := &codexTicketPoolDialer{}
	pool.setClientDialerForTest(dialer)
	conn, err := pool.dialConn(context.Background(), req)
	require.NoError(t, err)
	defer conn.close()
	require.Equal(t, "session=account", dialer.headers[0].Get("Cookie"))
}

func TestOpenAIWSCookiePolicyChangedBeforePrewarmRejectsOldSnapshot(t *testing.T) {
	for _, managed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unmanaged", true: "ticket"}[managed], func(t *testing.T) {
			f := newCodexCookieProjectionFixture(t, CodexCookiePreserve)
			headers := http.Header{}
			receipt, err := f.service.applyOpenAICodexTicketSnapshot(context.Background(), f.account, f.ticket.Model, headers)
			require.NoError(t, err)
			req := openAIWSAcquireRequest{Account: f.account, WSURL: "wss://chatgpt.com/backend-api/codex/responses", Headers: headers}
			if managed {
				req.CodexTicketReceipt = codexTicketWSReceiptFromSnapshot(receipt)
			}
			f.service.applyCodexRouteManagementPolicy(context.Background(), CodexRequestStrategyScopeDedicated, &req)
			pool := newOpenAIWSConnPool(f.service.cfg)
			t.Cleanup(pool.Close)
			dialer := &codexTicketPoolDialer{}
			pool.setClientDialerForTest(dialer)
			ap := pool.getOrCreateAccountPool(f.account.ID)
			ap.lastAcquire, ap.creating = cloneOpenAIWSAcquireRequestPtr(&req), 1
			f.policy(t, CodexCookieStripInfrastructure, CodexRequestStrategyScopeAll)
			pool.prewarmConns(f.account.ID, req, 1)
			require.Empty(t, dialer.headers, "保存新策略后，延迟预热不能发送旧 Cookie")
			require.Empty(t, ap.conns)
		})
	}
}

func TestOpenAIWSCookiePolicyDialUsesCapturedScope(t *testing.T) {
	f := newCodexCookieProjectionFixture(t, CodexCookieStripRouting)
	ticket := codexTicketLeaf(f.ticket)
	ticket.CookieMode, ticket.CookiePolicyVerified = CodexCookieStripRouting, true
	cfg := f.service.openAICodexTicketConfigForAccount(context.Background(), f.account)
	proofKey, err := f.service.codexCookieProjectionKey(f.account, ticket, cfg, CodexCookieStripRouting, "tok")
	require.NoError(t, err)
	ticket.CookiePolicyProofKey = proofKey
	f.service.openaiCodexCookieProjections.remember(proofKey, ticket.ExpiresAt)
	req := openAIWSAcquireRequest{Account: f.account, CodexTicketReceipt: &openAICodexTicketWSReceipt{
		account: f.account, ticket: *ticket, config: cfg, service: f.service},
		CookiePolicyScope: CodexRequestStrategyScopePassthrough,
		CookiePolicyCurrent: func(ctx context.Context) string {
			if codexRequestStrategyConnectionScope(ctx) == CodexRequestStrategyScopePassthrough {
				return CodexCookieStripRouting
			}
			return CodexCookiePreserve
		}}
	require.NoError(t, validateOpenAIWSCookiePolicyDial(context.Background(), req), "预热 background context 仍应保留 passthrough 范围")
	req.CookiePolicyScope = CodexRequestStrategyScopeDedicated
	require.ErrorIs(t, validateOpenAIWSCookiePolicyDial(context.Background(), req), ErrOpenAICodexTicketUnavailable)
	require.NoError(t, req.CodexTicketReceipt.validate(time.Now()), "策略变更不重写现有连接的原握手")
}
