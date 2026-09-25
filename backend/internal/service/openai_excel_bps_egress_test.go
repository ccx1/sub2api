//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

const excelBPSEgressCompletedWire = "event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_excel\",\"status\":\"completed\",\"model\":\"gpt-5.6-sol\",\"output\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}}\n\n"

type excelBPSEgressUpstream struct {
	do          func(*http.Request, string) (*http.Response, error)
	proxies     []string
	tlsAttempts int
}

func (u *excelBPSEgressUpstream) Do(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	u.proxies = append(u.proxies, proxy)
	return u.do(req, proxy)
}

func (u *excelBPSEgressUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.tlsAttempts++
	return u.Do(req, proxy, id, concurrency)
}

func excelBPSEgressSSE(wire string) *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}
}

type excelBPSRandomProxyRepo struct {
	AccountRepository
	failures, successes []int64
}

func (r *excelBPSRandomProxyRepo) ReportRandomProxyFailure(_ context.Context, _ int64, proxyID int64) error {
	r.failures = append(r.failures, proxyID)
	return nil
}

func (r *excelBPSRandomProxyRepo) ReportRandomProxySuccess(_ context.Context, _ int64, proxyID int64) error {
	r.successes = append(r.successes, proxyID)
	return nil
}

func excelBPSEgressService(upstream HTTPUpstream) *OpenAIGatewayService {
	s := &OpenAIGatewayService{httpUpstream: upstream, cfg: &config.Config{Security: config.SecurityConfig{
		URLAllowlist: config.URLAllowlistConfig{Enabled: false},
	}}}
	s.openaiProxyStreamCircuit = newOpenAIProxyStreamCircuit(openAIProxyStreamCircuitSettings{
		failureThreshold: 1, failureWindow: time.Minute, quarantineTTL: time.Minute, maxEntries: 16,
	})
	return s
}

func excelBPSProxiedAccount(mode string) *Account {
	account := excelAccount()
	proxy := proxyForTest(91, "primary.invalid", 8080)
	proxy.Name, proxy.FallbackMode = "primary", mode
	account.Proxy, account.ProxyID = proxy, i64(proxy.ID)
	return account
}

func forwardExcelBPSEgress(t *testing.T, s *OpenAIGatewayService, account *Account) (*httptest.ResponseRecorder, *gin.Context, error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, err := s.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.6-sol","stream":true,"input":"x"}`))
	return rec, c, err
}

func TestExcelBPSFixedProxyRefusalFallsBackToDirect(t *testing.T) {
	account := excelBPSProxiedAccount(FallbackModeDirect)
	upstream := &excelBPSEgressUpstream{do: func(req *http.Request, proxy string) (*http.Response, error) {
		require.Equal(t, basispoints.ResponsesURL, req.URL.String())
		if proxy != "" {
			return runtimeChainRefused(req)
		}
		return excelBPSEgressSSE(excelBPSEgressCompletedWire), nil
	}}
	rec, _, err := forwardExcelBPSEgress(t, excelBPSEgressService(upstream), account)
	require.NoError(t, err)
	require.Equal(t, []string{account.Proxy.URL(), ""}, upstream.proxies)
	require.Contains(t, rec.Body.String(), "response.completed")
}

func TestExcelBPSTransportErrorRecordsEgressAttribution(t *testing.T) {
	account := excelBPSProxiedAccount(FallbackModeNone)
	upstream := &excelBPSEgressUpstream{do: func(*http.Request, string) (*http.Response, error) {
		return nil, syscall.ECONNREFUSED
	}}
	rec, c, err := forwardExcelBPSEgress(t, excelBPSEgressService(upstream), account)
	require.Error(t, err)
	require.Len(t, upstream.proxies, 1, "BPS POST must not be replayed after a non-provable failure")
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Contains(t, rec.Body.String(), "basispoints_transport_error")
	events, ok := c.Get(OpsUpstreamErrorsKey)
	require.True(t, ok)
	attempts := events.([]*OpsUpstreamErrorEvent)
	require.Len(t, attempts, 1)
	require.Equal(t, "request_error", attempts[0].Kind)
	require.Equal(t, basispoints.ResponsesURL, attempts[0].UpstreamURL)
	require.NotNil(t, attempts[0].ProxyID)
	require.Equal(t, int64(91), *attempts[0].ProxyID)
	require.Equal(t, "primary", attempts[0].ProxyName)
}

func TestExcelBPSRandomProxyReportsTransportFailureAndSuccess(t *testing.T) {
	for _, fail := range []bool{true, false} {
		t.Run(map[bool]string{true: "failure", false: "success"}[fail], func(t *testing.T) {
			account := excelBPSProxiedAccount("")
			account.Extra[ProxyModeExtraKey] = ProxyModeRandom
			repo := &excelBPSRandomProxyRepo{}
			s := excelBPSEgressService(&excelBPSEgressUpstream{do: func(*http.Request, string) (*http.Response, error) {
				if fail {
					return nil, &net.OpError{Op: "dial", Err: errors.New("connection refused")}
				}
				return excelBPSEgressSSE(excelBPSEgressCompletedWire), nil
			}})
			s.accountRepo = repo
			_, _, err := forwardExcelBPSEgress(t, s, account)
			if fail {
				require.Error(t, err)
				require.Equal(t, []int64{91}, repo.failures)
				require.Empty(t, repo.successes)
				return
			}
			require.NoError(t, err)
			require.Empty(t, repo.failures)
			require.Equal(t, []int64{91}, repo.successes)
		})
	}
}

func TestExcelBPSBypassesOAuthPlugin(t *testing.T) {
	manager := &PluginManager{}
	manager.route.Store(&pluginRoute{pluginID: 1, rolloutPercent: 100, unavailable: "测试不可用"})
	upstream := &excelBPSEgressUpstream{do: func(*http.Request, string) (*http.Response, error) {
		return excelBPSEgressSSE(excelBPSEgressCompletedWire), nil
	}}
	s := excelBPSEgressService(upstream)
	s.pluginManager = manager
	_, _, err := forwardExcelBPSEgress(t, s, excelAccount())
	require.NoError(t, err)
	require.Len(t, upstream.proxies, 1, "an enabled OAuth plugin must not take over BPS traffic")

	require.False(t, openAIPluginBypassed(context.Background()))
	require.True(t, openAIPluginBypassed(withOpenAIPluginBypass(context.Background())))
}

func TestExcelBPSStreamDisconnectQuarantinesAndCompletionHeals(t *testing.T) {
	account := excelBPSProxiedAccount(FallbackModeNone)
	wire := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_excel\"}}\n\n"
	upstream := &excelBPSEgressUpstream{do: func(*http.Request, string) (*http.Response, error) {
		return excelBPSEgressSSE(wire), nil
	}}
	s := excelBPSEgressService(upstream)
	rec, _, err := forwardExcelBPSEgress(t, s, account)
	require.Error(t, err)
	require.Contains(t, rec.Body.String(), "basispoints_stream_incomplete")
	require.True(t, s.getOpenAIProxyStreamCircuit().isBlocked(91, time.Now()))

	wire = excelBPSEgressCompletedWire
	_, _, err = forwardExcelBPSEgress(t, s, account)
	require.NoError(t, err)
	require.False(t, s.getOpenAIProxyStreamCircuit().isBlocked(91, time.Now()))
}
