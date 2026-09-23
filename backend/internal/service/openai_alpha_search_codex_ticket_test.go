package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type alphaSearchTicketFixture struct {
	service  *OpenAIGatewayService
	account  *Account
	upstream *httpUpstreamRecorder
	client   *gin.Context
}

func newAlphaSearchTicketFixture(t *testing.T, mode string) *alphaSearchTicketFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body: io.NopCloser(strings.NewReader(strings.ReplaceAll(alphaSearchResponsesSSE("search result"),
			`"response":{"output":`, `"response":{"model":"gpt-6-astra","output":`))),
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, FailClosed: true, CredentialMode: mode, Models: []string{"gpt-6-astra"},
	}, upstream)
	account := ticketTestAccount(41)
	account.Credentials["access_token"] = "at-test-token"
	account.Credentials["auth_mode"] = OpenAIAuthModePersonalAccessToken
	account.Credentials["model_mapping"] = map[string]any{"search-alias": "gpt-6-astra"}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/alpha/search", nil)
	return &alphaSearchTicketFixture{service: svc, account: account, upstream: upstream, client: c}
}

func (f *alphaSearchTicketFixture) forward() (*OpenAIForwardResult, error) {
	return f.service.ForwardAlphaSearch(context.Background(), f.client, f.account,
		[]byte(`{"id":"search-session","model":"search-alias","commands":{"search_query":[{"q":"news"}]}}`))
}

func (f *alphaSearchTicketFixture) publish(t *testing.T, expiry time.Time) *openAICodexTicket {
	t.Helper()
	cfg := f.service.openAICodexTicketConfigForAccount(context.Background(), f.account)
	ticket := &openAICodexTicket{
		AccountID: f.account.ID, Model: "gpt-6-astra", CredentialMode: cfg.CredentialMode,
		State: fakeCodexTicketState(292), Length: 292, Verified: true,
		CapturedAt: time.Now().Add(-time.Minute), ExpiresAt: expiry,
		SessionID: "ticket-search-session",
	}
	if cfg.CredentialMode == config.CodexTicketCredentialCookie {
		ticket.State, ticket.Length = "", 0
	}
	if config.CodexTicketUsesCookies(cfg) {
		ticket.Cookies = []*http.Cookie{{Name: "ticket_session", Value: "search-credential",
			Path: "/backend-api", Secure: true, Expires: expiry}}
	}
	ticket.Binding = f.service.codexTicketBindingForConfig(f.account, cfg)
	require.True(t, f.service.storeOpenAICodexTicket(context.Background(), f.account, ticket))
	return ticket
}

func TestForwardAlphaSearchPATCodexTicketRequiresReadyCredential(t *testing.T) {
	for _, mode := range []string{"state", "cookie_state", "cookie"} {
		for _, expired := range []bool{false, true} {
			t.Run(mode+map[bool]string{false: "/missing", true: "/expired"}[expired], func(t *testing.T) {
				f := newAlphaSearchTicketFixture(t, mode)
				if expired {
					ticket := f.publish(t, time.Now().Add(time.Minute))
					ticket.ExpiresAt = time.Now().Add(-time.Second)
					f.service.openaiCodexTickets.Store(openAICodexTicketKey(f.account.ID, ticket.Model), ticket)
				}
				result, err := f.forward()
				require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
				require.Nil(t, result)
				require.Nil(t, f.upstream.lastReq, "映射后的模型无有效票时不能发出搜索请求")
			})
		}
	}
}

func TestForwardAlphaSearchPATCodexTicketCarriesCredential(t *testing.T) {
	for _, mode := range []string{"state", "cookie_state", "cookie"} {
		t.Run(mode, func(t *testing.T) {
			f := newAlphaSearchTicketFixture(t, mode)
			ticket := f.publish(t, time.Now().Add(time.Minute))
			result, err := f.forward()
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 1, result.WebSearchCalls)
			req := f.upstream.lastReq
			require.NotNil(t, req)
			require.Equal(t, chatgptCodexURL, req.URL.String())
			require.Equal(t, "gpt-6-astra", gjson.GetBytes(f.upstream.lastBody, "model").String())
			require.Equal(t, ticket.State, req.Header.Get(openAICodexTurnStateHeader))
			require.Equal(t, ticket.SessionID, req.Header.Get("Session_ID"))
			require.Equal(t, "Bearer at-test-token", req.Header.Get("Authorization"))
			require.NotNil(t, req.Context().Value(openAICodexTicketReceiptKey{}))
			if mode == "state" {
				require.Empty(t, req.Header.Get("Cookie"))
			} else {
				require.Equal(t, "ticket_session=search-credential", req.Header.Get("Cookie"))
			}
		})
	}
}

func TestForwardAlphaSearchPATCodexTicketPreservesUngatedRequests(t *testing.T) {
	for _, name := range []string{"other_outbound_model", "global_disabled", "account_disabled", "fail_open"} {
		t.Run(name, func(t *testing.T) {
			f := newAlphaSearchTicketFixture(t, "cookie_state")
			switch name {
			case "other_outbound_model":
				f.service.cfg.Gateway.OpenAICodexTicket.Models = []string{"search-alias"}
			case "global_disabled":
				f.service.cfg.Gateway.OpenAICodexTicket.Enabled = false
			case "account_disabled":
				f.account.Extra = map[string]any{OpenAICodexTicketEnabledExtraKey: false}
			case "fail_open":
				f.service.cfg.Gateway.OpenAICodexTicket.FailClosed = false
			}
			result, err := f.forward()
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, f.upstream.lastReq)
			require.Empty(t, f.upstream.lastReq.Header.Get("Cookie"))
			require.Empty(t, f.upstream.lastReq.Header.Get(openAICodexTurnStateHeader))
			require.Nil(t, f.upstream.lastReq.Context().Value(openAICodexTicketReceiptKey{}))
		})
	}
}
