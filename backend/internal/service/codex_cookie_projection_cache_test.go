package service

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexCookieProjectionCacheReusesOnlyMatchingProof(t *testing.T) {
	f := newCodexCookieProjectionFixture(t, CodexCookieStripRouting)
	calls := 0
	f.service.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls++
		return codexTicketCompletedResponse(f.ticket.Model, ""), nil
	}}
	prepare := func() {
		cfg := f.service.openAICodexTicketConfigForAccount(context.Background(), f.account)
		projected, err := f.service.prepareCodexCookieTicket(context.Background(), f.account, f.ticket, cfg)
		require.NoError(t, err)
		require.True(t, projected.CookiePolicyVerified)
		require.False(t, f.ticket.CookiePolicyVerified)
	}
	prepare()
	prepare()
	require.Equal(t, 1, calls, "相同票与模式复用验证结果")
	f.policy(t, CodexCookieStripCloudflare, CodexRequestStrategyScopeAll)
	prepare()
	require.Equal(t, 2, calls, "模式变化必须重新验证")
	f.account.Credentials["access_token"] = "refreshed-token"
	prepare()
	require.Equal(t, 3, calls, "账号 token 变化必须重新验证")
	f.service.cfg.Gateway.OpenAICodexTicket.BusinessVerificationRounds = 2
	f.service.settingService.codexTicketSettingsCache.Store(nil)
	prepare()
	require.Equal(t, 5, calls, "验证轮数变化不能复用单轮证明")
	prepare()
	require.Equal(t, 5, calls)
	f.account = ticketTestAccount(42)
	f.ticket = codexTicketLeaf(f.ticket)
	f.ticket.AccountID, f.ticket.AccountBinding, f.ticket.Binding = 42, "", ""
	prepare()
	require.Equal(t, 7, calls, "另一账号相同 Cookie 仍须独立验证")
}

func TestCodexCookieProjectionCacheExpiryAndRevocation(t *testing.T) {
	f := newCodexCookieProjectionFixture(t, CodexCookieStripInfrastructure)
	calls := 0
	f.service.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls++
		return codexTicketCompletedResponse(f.ticket.Model, ""), nil
	}}
	cfg := f.service.openAICodexTicketConfigForAccount(context.Background(), f.account)
	_, err := f.service.prepareCodexCookieTicket(context.Background(), f.account, f.ticket, cfg)
	require.NoError(t, err)
	f.service.openaiCodexCookieProjections.mu.Lock()
	for key := range f.service.openaiCodexCookieProjections.proofs {
		f.service.openaiCodexCookieProjections.proofs[key] = time.Now().Add(-time.Second)
	}
	f.service.openaiCodexCookieProjections.mu.Unlock()
	_, err = f.service.prepareCodexCookieTicket(context.Background(), f.account, f.ticket, cfg)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
	req := f.request(t, context.Background())
	require.NoError(t, f.service.applyOpenAICodexTicketRequest(f.account, f.ticket.Model, req))
	f.service.rememberCodexTicketRevocation(openAICodexTicketKey(f.account.ID, f.ticket.Model), f.ticket)
	_, err = f.service.prepareCodexCookieTicket(context.Background(), f.account, f.ticket, cfg)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	resp, err := f.service.doOpenAIUpstream(req, "", f.account)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	require.Nil(t, resp)
	require.Equal(t, 2, calls, "已撤销原票不能被缓存证明复活")
}

func TestCodexCookieProjectionScopeAndPolicyChangeBeforeSend(t *testing.T) {
	f := newCodexCookieProjectionFixture(t, CodexCookieStripInfrastructure)
	f.policy(t, CodexCookieStripInfrastructure, CodexRequestStrategyScopeDedicated)
	calls := 0
	f.service.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		calls++
		return codexTicketCompletedResponse(f.ticket.Model, ""), nil
	}}
	passthrough := withCodexRequestStrategyConnectionScope(context.Background(), CodexRequestStrategyScopePassthrough)
	req := f.request(t, passthrough)
	require.NoError(t, f.service.applyOpenAICodexTicketRequest(f.account, f.ticket.Model, req))
	require.Contains(t, req.Header.Get("Cookie"), "__oailb=route")
	require.Zero(t, calls)
	req = f.request(t, context.Background())
	require.NoError(t, f.service.applyOpenAICodexTicketRequest(f.account, f.ticket.Model, req))
	require.NotContains(t, req.Header.Get("Cookie"), "__oailb=")
	require.Equal(t, 1, calls)
	f.policy(t, CodexCookieStripRouting, CodexRequestStrategyScopeDedicated)
	resp, err := f.service.doOpenAIUpstream(req, "", f.account)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	require.Nil(t, resp)
	require.Equal(t, 1, calls, "策略改变后旧 receipt 不能发送业务")
}

func TestCodexCookieProjectionFailedProbeDoesNotPoisonCache(t *testing.T) {
	f := newCodexCookieProjectionFixture(t, CodexCookieStripRouting)
	calls := 0
	f.service.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			return nil, io.EOF
		}
		return codexTicketCompletedResponse(f.ticket.Model, ""), nil
	}}
	cfg := f.service.openAICodexTicketConfigForAccount(context.Background(), f.account)
	_, err := f.service.prepareCodexCookieTicket(context.Background(), f.account, f.ticket, cfg)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	_, err = f.service.prepareCodexCookieTicket(context.Background(), f.account, f.ticket, cfg)
	require.NoError(t, err)
	_, err = f.service.prepareCodexCookieTicket(context.Background(), f.account, f.ticket, cfg)
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestCodexCookieProjectionRejectsCredentialModeChangeBeforeSend(t *testing.T) {
	f := newCodexCookieProjectionFixture(t, CodexCookieStripInfrastructure)
	calls := 0
	f.service.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		calls++
		return codexTicketCompletedResponse(f.ticket.Model, ""), nil
	}}
	req := f.request(t, context.Background())
	require.NoError(t, f.service.applyOpenAICodexTicketRequest(f.account, f.ticket.Model, req))
	require.Equal(t, 1, calls, "projection probe should be the only upstream call so far")
	// 当前账号策略切换到无 Cookie 的 state 且 fail-open；旧 receipt 不能绕过
	// 当前凭据模式检查继续发送已经构造好的 Cookie 投影。
	f.service.cfg.Gateway.OpenAICodexTicket.CredentialMode = "state"
	f.service.cfg.Gateway.OpenAICodexTicket.FailClosed = false
	f.service.settingService.codexTicketSettingsCache.Store(nil)
	resp, err := f.service.doOpenAIUpstream(req, "", f.account)
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	require.Nil(t, resp)
	require.Equal(t, 1, calls, "凭据模式变化后不得发送业务请求")
}

func TestCodexCookieProjectionReceiptRejectsTokenOrVerificationRoundChange(t *testing.T) {
	for _, change := range []string{"token", "verification_rounds"} {
		t.Run(change, func(t *testing.T) {
			f := newCodexCookieProjectionFixture(t, CodexCookieStripInfrastructure)
			calls := 0
			f.service.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				calls++
				return codexTicketCompletedResponse(f.ticket.Model, ""), nil
			}}
			req := f.request(t, context.Background())
			require.NoError(t, f.service.applyOpenAICodexTicketRequest(f.account, f.ticket.Model, req))
			require.Equal(t, 1, calls)
			switch change {
			case "token":
				f.account.Credentials["access_token"] = "rotated-token"
			case "verification_rounds":
				f.service.cfg.Gateway.OpenAICodexTicket.BusinessVerificationRounds = 2
				f.service.settingService.codexTicketSettingsCache.Store(nil)
			}
			resp, err := f.service.doOpenAIUpstream(req, "", f.account)
			require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
			require.Nil(t, resp)
			require.Equal(t, 1, calls, "变化后的验证组合不能发送旧 receipt")
		})
	}
}
