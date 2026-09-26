//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"syscall"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func forwardExcelBPSRepairTransport(t *testing.T, svc *OpenAIGatewayService, account *Account) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.6-sol","stream":true,"input":"test correction egress","tools":[{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"exec"}]}]}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	_, err := svc.Forward(context.Background(), c, account, body)
	require.NoError(t, err)
	require.Equal(t, 1, strings.Count(rec.Body.String(), "event: response.completed"))
	require.Contains(t, rec.Body.String(), `"input":"text(42);"`)
	return rec
}

func TestExcelBPSToolCorrectionPreservesProtectedRandomProxyEgress(t *testing.T) {
	account := excelBPSProxiedAccount(FallbackModeNone)
	account.Extra[ProxyModeExtraKey] = ProxyModeRandom
	configureAccountProtection(account)
	profile, err := resolveMode1TLSProfile(account)
	require.NoError(t, err)
	require.NotNil(t, profile)
	first := excelBPSRepairWire(t, "protected_initial", "Run", "text(42);")
	corrected := excelBPSRepairWire(t, "protected_corrected", "codex2api.custom/functions.exec", "text(42);")
	var bypassed []bool
	upstream := &excelBPSEgressUpstream{}
	upstream.do = func(req *http.Request, _ string) (*http.Response, error) {
		bypassed = append(bypassed, openAIPluginBypassed(req.Context()))
		if len(upstream.proxies) == 1 {
			return excelBPSEgressSSE(first), nil
		}
		return excelBPSEgressSSE(corrected), nil
	}
	repo := &excelBPSRandomProxyRepo{}
	svc := excelBPSEgressService(upstream)
	svc.accountRepo = repo
	svc.cfg.Gateway.TLSFingerprint.Enabled = true
	svc.pluginManager = &PluginManager{}
	svc.pluginManager.route.Store(&pluginRoute{pluginID: 1, rolloutPercent: 100, unavailable: "测试不可用"})

	forwardExcelBPSRepairTransport(t, svc, account)

	require.Equal(t, []string{account.Proxy.URL(), account.Proxy.URL()}, upstream.proxies)
	require.Equal(t, 2, upstream.tlsAttempts, "the correction must retain the account TLS profile")
	require.Equal(t, []bool{true, true}, bypassed, "both requests must bypass OAuth plugins")
	require.Equal(t, []int64{91, 91}, repo.successes, "record random proxy health for both requests")
	require.Empty(t, repo.failures)
}

func TestExcelBPSToolCorrectionRetainsFixedProxyFallback(t *testing.T) {
	account := excelBPSProxiedAccount(FallbackModeDirect)
	first := excelBPSRepairWire(t, "fallback_initial", "Run", "text(42);")
	corrected := excelBPSRepairWire(t, "fallback_corrected", "codex2api.custom/functions.exec", "text(42);")
	upstream := &excelBPSEgressUpstream{}
	upstream.do = func(req *http.Request, proxy string) (*http.Response, error) {
		if len(upstream.proxies) == 1 {
			return excelBPSEgressSSE(first), nil
		}
		if proxy != "" {
			if trace := httptrace.ContextClientTrace(req.Context()); trace != nil && trace.GetConn != nil {
				trace.GetConn("primary.invalid:8080")
			}
			return nil, syscall.ECONNREFUSED
		}
		return excelBPSEgressSSE(corrected), nil
	}

	forwardExcelBPSRepairTransport(t, excelBPSEgressService(upstream), account)

	require.Equal(t, []string{account.Proxy.URL(), account.Proxy.URL(), ""}, upstream.proxies,
		"a correction refused before sending HTTP may use the account's authorized direct fallback")
}
