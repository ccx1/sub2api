package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexCookieProjectionFixture struct {
	service *OpenAIGatewayService
	account *Account
	ticket  *openAICodexTicket
	repo    *codexTicketSettingRepo
}

func newCodexCookieProjectionFixture(t *testing.T, mode string) codexCookieProjectionFixture {
	t.Helper()
	s := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, CredentialMode: config.CodexTicketCredentialCookie,
		CookieTTLSeconds: 300, TTLSeconds: 300, BusinessVerificationRounds: 1, FailClosed: true}, nil)
	repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{}}}
	s.settingService = NewSettingService(repo, s.cfg)
	f := codexCookieProjectionFixture{service: s, account: ticketTestAccount(41), repo: repo}
	f.policy(t, mode, CodexRequestStrategyScopeAll)
	expires := time.Now().Add(5 * time.Minute)
	f.ticket = &openAICodexTicket{AccountID: 41, Model: "gpt-6-astra", CredentialMode: config.CodexTicketCredentialCookie,
		Verified: true, SessionID: "projection-session", CapturedAt: time.Now(), ExpiresAt: expires}
	for _, cookie := range []*http.Cookie{
		{Name: "__oailb", Value: "route"}, {Name: "__cflb", Value: "lb"},
		{Name: "__cf_bm", Value: "bot"}, {Name: "cf_clearance", Value: "clearance"},
		{Name: "chatgpt_session", Value: "account=="}, {Name: "oai-auth-token", Value: "auth"},
		{Name: "__Secure-next-auth.session-token", Value: "secure-auth"}, {Name: "unknown", Value: "kept"},
	} {
		cookie.Path, cookie.Secure, cookie.Expires = "/backend-api", true, expires
		f.ticket.Cookies = append(f.ticket.Cookies, cookie)
	}
	f.ticket.Binding = s.codexTicketBindingForConfig(f.account, s.openAICodexTicketConfigForAccount(context.Background(), f.account))
	require.True(t, s.storeOpenAICodexTicket(context.Background(), f.account, f.ticket))
	f.ticket = s.lookupOpenAICodexTicket(f.account, f.ticket.Model)
	require.NotNil(t, f.ticket)
	return f
}

func (f codexCookieProjectionFixture) policy(t *testing.T, mode, scope string) {
	t.Helper()
	policy := DefaultCodexRequestStrategyPolicy()
	policy.Enabled, policy.CookieMode, policy.Scope = true, mode, scope
	raw, err := json.Marshal(policy)
	require.NoError(t, err)
	f.repo.values[SettingKeyCodexRequestStrategy] = string(raw)
}

func (f codexCookieProjectionFixture) request(t *testing.T, ctx context.Context) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexURL, strings.NewReader(`{"model":"gpt-6-astra","input":"business"}`))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+f.account.GetCredential("access_token"))
	return req
}

func TestCodexCookieProjectionProbeAndBusinessUseSameHeaders(t *testing.T) {
	for _, mode := range []string{CodexCookiePreserve, CodexCookieStripRouting, CodexCookieStripCloudflare, CodexCookieStripInfrastructure} {
		t.Run(mode, func(t *testing.T) {
			f := newCodexCookieProjectionFixture(t, mode)
			before, err := json.Marshal(f.ticket)
			require.NoError(t, err)
			var sent []http.Header
			f.service.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				sent = append(sent, req.Header.Clone())
				response := codexTicketCompletedResponse(f.ticket.Model, "")
				response.Header.Add("Set-Cookie", "__oailb=new-route; Path=/backend-api; Secure")
				return response, nil
			}}
			req := f.request(t, context.Background())
			require.NoError(t, f.service.applyOpenAICodexTicketRequest(f.account, f.ticket.Model, req))
			resp, err := f.service.doOpenAIUpstream(req, "", f.account)
			require.NoError(t, err)
			_, err = io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			wantCalls := 2
			if mode == CodexCookiePreserve {
				wantCalls = 1
			}
			require.Len(t, sent, wantCalls)
			for _, headers := range sent {
				require.Equal(t, "Bearer tok", headers.Get("Authorization"))
				require.Equal(t, f.ticket.SessionID, headers.Get("session_id"))
				for _, cookie := range f.ticket.Cookies {
					_, err := (&http.Request{Header: headers}).Cookie(cookie.Name)
					require.Equal(t, codexCookieExcluded(mode, cookie.Name), err == http.ErrNoCookie, cookie.Name)
				}
				require.Equal(t, sent[0].Get("Cookie"), headers.Get("Cookie"))
			}
			after, err := json.Marshal(f.service.lookupOpenAICodexTicket(f.account, f.ticket.Model))
			require.NoError(t, err)
			require.JSONEq(t, string(before), string(after), "响应新 Cookie 不能修改已发布原票")
		})
	}
}

func TestCodexCookieProjectionVerificationFailureNeverSendsBusiness(t *testing.T) {
	for _, failClosed := range []bool{false, true} {
		f := newCodexCookieProjectionFixture(t, CodexCookieStripInfrastructure)
		f.service.cfg.Gateway.OpenAICodexTicket.FailClosed = failClosed
		f.service.settingService.codexTicketSettingsCache.Store(nil)
		calls := 0
		f.service.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
			calls++
			body, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Contains(t, string(body), "ping")
			return codexTicketCompletedResponse("gpt-other", ""), nil
		}}
		req := f.request(t, context.Background())
		require.ErrorIs(t, f.service.applyOpenAICodexTicketRequest(f.account, f.ticket.Model, req), ErrOpenAICodexTicketUnavailable)
		require.Equal(t, 1, calls)
		require.Empty(t, req.Header.Get("Cookie"))
		require.Nil(t, req.Context().Value(openAICodexTicketReceiptKey{}))
		require.Empty(t, f.service.openaiCodexCookieProjections.proofs)
	}
}

func TestCodexCookieProjectionEmptyFilteredCookiesStillNeedValidTicket(t *testing.T) {
	f := newCodexCookieProjectionFixture(t, CodexCookieStripInfrastructure)
	f.ticket.Cookies = f.ticket.Cookies[:4]
	f.service.openaiCodexTickets.Store(openAICodexTicketKey(f.account.ID, f.ticket.Model), f.ticket)
	calls := 0
	f.service.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		calls++
		require.Empty(t, req.Header.Get("Cookie"))
		return codexTicketCompletedResponse(f.ticket.Model, ""), nil
	}}
	req := f.request(t, context.Background())
	require.NoError(t, f.service.applyOpenAICodexTicketRequest(f.account, f.ticket.Model, req))
	receipt, ok := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	require.True(t, ok)
	require.True(t, receipt.ticket.matchesHeaders(req.Header))
	require.NoError(t, f.service.validateOpenAICodexTicketSend(req, f.account))
	require.Equal(t, 1, calls)
	for _, target := range []string{"http://chatgpt.com/backend-api/codex/responses", "https://chatgpt.com/other", "https://other.test/backend-api/codex/responses"} {
		invalid := req.Clone(req.Context())
		invalid.URL, _ = url.Parse(target)
		require.ErrorIs(t, f.service.validateOpenAICodexTicketSend(invalid, f.account), ErrOpenAICodexTicketUnavailable)
	}
	receipt.ticket.ExpiresAt = time.Now().Add(-time.Second)
	require.ErrorIs(t, f.service.validateOpenAICodexTicketSend(req, f.account), ErrOpenAICodexTicketUnavailable)
	f.service.openaiCodexTickets.Delete(openAICodexTicketKey(f.account.ID, f.ticket.Model))
	require.ErrorIs(t, f.service.applyOpenAICodexTicketRequest(f.account, f.ticket.Model, f.request(t, context.Background())), ErrOpenAICodexTicketUnavailable)
	require.Equal(t, 1, calls)
}

func TestCodexCookieProjectionLegacySessionAndRevokedTicketsReject(t *testing.T) {
	for _, kind := range []string{"legacy", "expired", "revoked", "empty_raw"} {
		t.Run(kind, func(t *testing.T) {
			f := newCodexCookieProjectionFixture(t, CodexCookieStripRouting)
			f.service.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid ticket must fail before probing")
				return nil, io.EOF
			}}
			switch kind {
			case "legacy":
				f.ticket.SessionID = ""
			case "expired":
				f.ticket.ExpiresAt = time.Now().Add(-time.Second)
			case "revoked":
				f.service.rememberCodexTicketRevocation(openAICodexTicketKey(f.account.ID, f.ticket.Model), f.ticket)
			case "empty_raw":
				f.ticket.Cookies = nil
			}
			cfg := f.service.openAICodexTicketConfigForAccount(context.Background(), f.account)
			_, err := f.service.prepareCodexCookieTicket(context.Background(), f.account, f.ticket, cfg)
			require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
		})
	}
}

func TestCodexCookieProjectionProbeCapturesResponseCookiesAsCandidates(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusTooManyRequests} {
		f := newCodexCookieProjectionFixture(t, CodexCookieStripInfrastructure)
		f.service.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
			require.NotContains(t, req.Header.Get("Cookie"), "__oailb=")
			resp := codexTicketCompletedResponse(f.ticket.Model, "")
			resp.StatusCode = status
			resp.Header.Add("Set-Cookie", "__oailb=candidate-route; Path=/backend-api; Max-Age=120; Secure")
			return resp, nil
		}}
		cfg := f.service.openAICodexTicketConfigForAccount(context.Background(), f.account)
		_, err := f.service.prepareCodexCookieTicket(context.Background(), f.account, f.ticket, cfg)
		if status == http.StatusOK {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
		}
		pending := f.service.pendingCodexTicketCookies(f.account.ID, f.ticket.Model)
		require.NotNil(t, pending, "status=%d", status)
		u, err := url.Parse(chatgptCodexURL)
		require.NoError(t, err)
		require.Contains(t, codexTicketCookieHeaderValues(pending.Candidate.Jar.Cookies(u)), "__oailb=candidate-route")
		require.Equal(t, "route", f.service.lookupOpenAICodexTicket(f.account, f.ticket.Model).Cookies[0].Value)
		require.Equal(t, "route", f.ticket.Cookies[0].Value)
	}
}
