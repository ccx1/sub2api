package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegionalBypassRequiresResolvedDirectDecision(t *testing.T) {
	account := randomProxyAccount(RandomProxyEmptyPoolPolicyDirect)
	bypass := func() bool {
		return RegionalEgressBypassFromContext(WithRegionalEgressBypassForAccount(t.Context(), account))
	}
	require.False(t, bypass(), "configuration alone must not bypass regional routing")
	require.NoError(t, ResolveRandomProxy(t.Context(), account, &randomProxySelectorStub{}))
	require.True(t, bypass())
	require.NoError(t, ResolveRandomProxy(t.Context(), account, &randomProxySelectorStub{proxy: &Proxy{ID: 7, Status: StatusActive}}))
	require.False(t, bypass(), "a later successful selection must clear the direct decision")
	require.Error(t, ResolveRandomProxy(t.Context(), account, &randomProxySelectorStub{err: errors.New("pool unavailable")}))
	require.False(t, bypass(), "selection failure must not become direct")
	for _, policy := range []string{RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDisable} {
		account.Extra[RandomProxyEmptyPoolPolicyExtraKey] = policy
		require.Error(t, ResolveRandomProxy(t.Context(), account, &randomProxySelectorStub{}))
		require.False(t, bypass())
	}
}

type regionalAccountTestUpstream struct {
	request *http.Request
}

func (u *regionalAccountTestUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.request = req
	return nil, errors.New("synthetic stop after egress selection")
}

func (u *regionalAccountTestUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

func TestOpenAIAccountTestPreservesResolvedDirectEgress(t *testing.T) {
	for _, direct := range []bool{false, true} {
		account := &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
		if direct {
			account.Extra = randomProxyAccount(RandomProxyEmptyPoolPolicyDirect).Extra
			require.NoError(t, ResolveRandomProxy(t.Context(), account, &randomProxySelectorStub{}))
		}
		upstream := &regionalAccountTestUpstream{}
		service := &AccountTestService{httpUpstream: upstream}
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://api.openai.com/v1/models", nil)
		require.NoError(t, err)
		_, err = service.doOpenAIAccountTestUpstream(request, "", account, false)
		require.Error(t, err)
		require.NotNil(t, upstream.request)
		require.Equal(t, direct, RegionalEgressBypassFromContext(upstream.request.Context()))
	}
}

func TestRegionalWebSocketTLSUsesSelectedEgress(t *testing.T) {
	for _, direct := range []bool{false, true} {
		t.Run(map[bool]string{false: "regional", true: "random-direct"}[direct], func(t *testing.T) {
			var targetHits, proxyHits atomic.Int32
			target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { targetHits.Add(1) }))
			defer target.Close()
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				proxyHits.Add(1)
				require.Equal(t, http.MethodConnect, r.Method)
				w.WriteHeader(http.StatusBadGateway)
			}))
			defer proxy.Close()
			dialer := newConfiguredOpenAIWSClientDialer(regionalWSConfig(t, proxy.URL, "127.0.0.1")).(openAIWSClientTLSDialer)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			if direct {
				account := randomProxyAccount(RandomProxyEmptyPoolPolicyDirect)
				require.NoError(t, ResolveRandomProxy(ctx, account, &randomProxySelectorStub{}))
				ctx = WithRegionalEgressBypassForAccount(ctx, account)
			}
			_, _, _, err := dialer.DialWithTLS(ctx, strings.Replace(target.URL, "https://", "wss://", 1), nil, "", tlsfingerprint.BuiltinProfile("nodejs22"))
			require.Error(t, err)
			require.Zero(t, targetHits.Load(), "neither a failed proxy nor an untrusted direct TLS certificate may send an HTTP request")
			if direct {
				require.Zero(t, proxyHits.Load())
			} else {
				require.EqualValues(t, 1, proxyHits.Load(), "TLS fingerprint transport must use the selected regional proxy")
			}
		})
	}
}

func TestRegionalWebSocketPoolSeparatesDirectDecision(t *testing.T) {
	account := randomProxyAccount(RandomProxyEmptyPoolPolicyDirect)
	ordinary := normalizeOpenAIWSTransportCompatibility(openAIWSAcquireRequest{Account: account})
	require.NoError(t, ResolveRandomProxy(context.Background(), account, &randomProxySelectorStub{}))
	direct := normalizeOpenAIWSTransportCompatibility(openAIWSAcquireRequest{Account: account})
	require.NotEqual(t, ordinary, direct, "a regional connection must not be reused for an explicit direct decision")
}

type regionalAccountRepository struct {
	AccountRepository
	account *Account
}

func (r *regionalAccountRepository) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func (r *regionalAccountRepository) SelectRandomActiveProxy(context.Context) (*Proxy, error) {
	return nil, nil
}

func TestAccountConnectionPreservesRegionalEgressAcrossProviders(t *testing.T) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI, PlatformGemini, PlatformGrok, PlatformTypeSafe} {
		for _, direct := range []bool{false, true} {
			t.Run(platform+map[bool]string{false: "/regional", true: "/direct"}[direct], func(t *testing.T) {
				account := &Account{ID: 42, Platform: platform, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "synthetic", "base_url": "https://api.test.invalid"}}
				if direct {
					account.Extra = randomProxyAccount(RandomProxyEmptyPoolPolicyDirect).Extra
				}
				upstream := &regionalAccountTestUpstream{}
				service := &AccountTestService{accountRepo: &regionalAccountRepository{account: account}, httpUpstream: upstream, cfg: &config.Config{}}
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
				require.Error(t, service.TestAccountConnection(c, account.ID, "", "", ""))
				require.NotNil(t, upstream.request, "account test must reach the shared upstream")
				require.Equal(t, direct, RegionalEgressBypassFromContext(upstream.request.Context()))
			})
		}
	}
}

func TestNonOpenAIForwardPreservesResolvedDirectEgress(t *testing.T) {
	account := randomProxyAccount(RandomProxyEmptyPoolPolicyDirect)
	account.Credentials = map[string]any{"base_url": "https://api.test.invalid", "api_key": "synthetic"}
	require.NoError(t, ResolveRandomProxy(t.Context(), account, &randomProxySelectorStub{}))
	upstream := &regionalAccountTestUpstream{}
	service := &AntigravityGatewayService{httpUpstream: upstream}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	_, err := service.ForwardUpstream(t.Context(), c, account, []byte(`{"model":"claude-test","messages":[{"role":"user","content":"hi"}]}`))
	require.Error(t, err)
	require.NotNil(t, upstream.request)
	require.True(t, RegionalEgressBypassFromContext(upstream.request.Context()))
}

func TestExplicitProxyFallbackBypassesRegionalRouting(t *testing.T) {
	account := &Account{ID: 42, Proxy: &Proxy{ID: 7, FallbackMode: FallbackModeDirect}}
	upstream := &regionalAccountTestUpstream{}
	service := &OpenAIGatewayService{httpUpstream: upstream}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "https://api.test.invalid", strings.NewReader("{}"))
	require.NoError(t, err)
	_, err = service.doOpenAIProxyAttempt(req, account, runtimeProxyEgress{proxyID: 0})
	require.Error(t, err)
	require.NotNil(t, upstream.request)
	require.True(t, RegionalEgressBypassFromContext(upstream.request.Context()))
}

func TestExcelBPSPreservesNativeEgressAndStreamProfile(t *testing.T) {
	upstream := &regionalAccountTestUpstream{}
	service := &OpenAIGatewayService{httpUpstream: upstream}
	ctx := WithHTTPUpstreamProfile(t.Context(), HTTPUpstreamProfileLongStream)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://bps.openai.com", strings.NewReader("{}"))
	require.NoError(t, err)
	_, err = service.doExcelBPSUpstream(req, "", &Account{ID: 42})
	require.Error(t, err)
	require.NotNil(t, upstream.request)
	require.True(t, RegionalEgressBypassFromContext(upstream.request.Context()))
	require.Equal(t, HTTPUpstreamProfileLongStream, HTTPUpstreamProfileFromContext(upstream.request.Context()))
}

func TestCodexTicketPreservesBoundDirectEgress(t *testing.T) {
	upstream := &regionalAccountTestUpstream{}
	service := &OpenAIGatewayService{httpUpstream: upstream}
	account := &Account{ID: 42}
	ticket := openAICodexTicket{State: "synthetic-state"}
	ctx := context.WithValue(t.Context(), openAICodexTicketReceiptKey{}, &openAICodexTicketReceipt{ticket: ticket})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://chatgpt.com", strings.NewReader("{}"))
	require.NoError(t, err)
	ticket.applyHeaders(req.Header)
	_, err = service.doOpenAIProxyAttempt(req, account, runtimeProxyEgress{proxyID: 0})
	require.Error(t, err)
	require.NotNil(t, upstream.request)
	require.True(t, RegionalEgressBypassFromContext(upstream.request.Context()))
	ordinary := normalizeOpenAIWSTransportCompatibility(openAIWSAcquireRequest{Account: account})
	bound := normalizeOpenAIWSTransportCompatibility(openAIWSAcquireRequest{Account: account, CodexTicketReceipt: &openAICodexTicketWSReceipt{ticket: ticket}})
	require.NotEqual(t, ordinary.proxyIdentity, bound.proxyIdentity)
}
