//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func forwardEncryptedExcelBPSEgress(t *testing.T, s *OpenAIGatewayService, account *Account) (*httptest.ResponseRecorder, *gin.Context, error) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	_, err := s.Forward(context.Background(), c, account, excelBPSEncryptedHistoryRequest(true))
	return rec, c, err
}

func assertEncryptedExcelBPSRetryProxy(t *testing.T, c *gin.Context, expected *Proxy) {
	t.Helper()
	raw, exists := c.Get(OpsUpstreamErrorsKey)
	require.True(t, exists)
	var retryEvents []*OpsUpstreamErrorEvent
	for _, event := range raw.([]*OpsUpstreamErrorEvent) {
		if event.Kind == "invalid_encrypted_content_retry" {
			retryEvents = append(retryEvents, event)
		}
	}
	require.Len(t, retryEvents, 1)
	if expected == nil {
		require.Nil(t, retryEvents[0].ProxyID)
		require.Equal(t, opsProxyNameDirect, retryEvents[0].ProxyName)
		return
	}
	require.NotNil(t, retryEvents[0].ProxyID)
	require.Equal(t, expected.ID, *retryEvents[0].ProxyID)
	require.Equal(t, expected.Name, retryEvents[0].ProxyName)
}

func encryptedExcelBPSRejection(body io.ReadCloser) *http.Response {
	return &http.Response{StatusCode: http.StatusBadRequest, Header: http.Header{}, Body: body}
}

func encryptedExcelBPSFallbackService(upstream HTTPUpstream, mode string) (*OpenAIGatewayService, *Account, *Proxy) {
	account := excelBPSProxiedAccount(mode)
	backup := proxyForTest(92, "backup.invalid", 8080)
	backup.Name, backup.FallbackMode = "backup", FallbackModeDirect
	account.Proxy.BackupProxyID = i64(backup.ID)
	svc := excelBPSEgressService(upstream)
	svc.proxyRepo = &fakeProxyLookup{byID: map[int64]*Proxy{backup.ID: backup}}
	return svc, account, backup
}

func TestExcelBPSInvalidEncryptedRetryPinsActualFallback(t *testing.T) {
	for _, mode := range []string{FallbackModeDirect, FallbackModeProxy} {
		t.Run(mode, func(t *testing.T) {
			upstream := &excelBPSEgressUpstream{}
			svc, account, backup := encryptedExcelBPSFallbackService(upstream, mode)
			primaryURL, actualURL := account.Proxy.URL(), ""
			if mode == FallbackModeProxy {
				actualURL = backup.URL()
			}
			first := &excelBPSRepairBody{Reader: strings.NewReader(excelBPSInvalidCiphertext)}
			upstream.do = func(req *http.Request, proxy string) (*http.Response, error) {
				switch len(upstream.proxies) {
				case 1:
					return runtimeChainRefused(req)
				case 2:
					// 回包后配置发生变化，恢复仍须使用请求内记录的真实 URL。
					account.Proxy.Host, backup.Host = "changed-primary.invalid", "changed-backup.invalid"
					return encryptedExcelBPSRejection(first), nil
				default:
					require.True(t, first.closed.Load(), "release first response before retry")
					body, err := io.ReadAll(req.Body)
					require.NoError(t, err)
					require.NotContains(t, string(body), "private-ciphertext")
					return excelBPSEgressSSE(excelBPSEgressCompletedWire), nil
				}
			}
			rec, c, err := forwardEncryptedExcelBPSEgress(t, svc, account)
			require.NoError(t, err)
			require.Equal(t, []string{primaryURL, actualURL, actualURL}, upstream.proxies)
			require.Contains(t, rec.Body.String(), "response.completed")
			if mode == FallbackModeDirect {
				backup = nil
			}
			assertEncryptedExcelBPSRetryProxy(t, c, backup)
		})
	}
}

func TestExcelBPSInvalidEncryptedRetryDoesNotFallbackAgain(t *testing.T) {
	for _, useBackup := range []bool{false, true} {
		t.Run(map[bool]string{false: "primary", true: "backup"}[useBackup], func(t *testing.T) {
			upstream := &excelBPSEgressUpstream{}
			svc, account, backup := encryptedExcelBPSFallbackService(upstream, FallbackModeDirect)
			expected := []string{account.Proxy.URL(), account.Proxy.URL()}
			rejectionAttempt := 1
			if useBackup {
				account.Proxy.FallbackMode = FallbackModeProxy
				expected = []string{account.Proxy.URL(), backup.URL(), backup.URL()}
				rejectionAttempt = 2
			}
			upstream.do = func(req *http.Request, _ string) (*http.Response, error) {
				if len(upstream.proxies) == rejectionAttempt {
					return encryptedExcelBPSRejection(io.NopCloser(strings.NewReader(excelBPSInvalidCiphertext))), nil
				}
				return runtimeChainRefused(req)
			}
			rec, _, err := forwardEncryptedExcelBPSEgress(t, svc, account)
			require.Error(t, err)
			require.Equal(t, expected, upstream.proxies, "recovery cannot switch egress after sending")
			require.Equal(t, http.StatusBadGateway, rec.Code)
			require.Contains(t, rec.Body.String(), "basispoints_transport_error")
		})
	}
}

type encryptedExcelBPSTLSUpstream struct {
	*excelBPSEgressUpstream
	concurrencies []int
}

func (u *encryptedExcelBPSTLSUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	u.concurrencies = append(u.concurrencies, concurrency)
	return u.excelBPSEgressUpstream.DoWithTLS(req, proxy, id, concurrency, profile)
}

func TestExcelBPSInvalidEncryptedRetryKeepsNativePolicies(t *testing.T) {
	account := excelBPSProxiedAccount(FallbackModeNone)
	account.Extra[ProxyModeExtraKey] = ProxyModeRandom
	account.Extra[AntiDegradationExtraKey] = true
	account.Extra["tls_fingerprint_builtin"] = "nodejs24"
	account.Concurrency = 0
	upstream := &encryptedExcelBPSTLSUpstream{excelBPSEgressUpstream: &excelBPSEgressUpstream{}}
	upstream.do = func(req *http.Request, _ string) (*http.Response, error) {
		require.True(t, openAIPluginBypassed(req.Context()))
		if len(upstream.proxies) == 1 {
			return encryptedExcelBPSRejection(io.NopCloser(strings.NewReader(excelBPSInvalidCiphertext))), nil
		}
		return excelBPSEgressSSE(excelBPSEgressCompletedWire), nil
	}
	svc := excelBPSEgressService(upstream)
	svc.cfg.Gateway.TLSFingerprint.Enabled = true
	repo := &excelBPSRandomProxyRepo{}
	svc.accountRepo = repo
	manager := &PluginManager{}
	manager.route.Store(&pluginRoute{pluginID: 1, rolloutPercent: 100, unavailable: "test unavailable"})
	svc.pluginManager = manager
	_, _, err := forwardEncryptedExcelBPSEgress(t, svc, account)
	require.NoError(t, err)
	require.Equal(t, 2, upstream.tlsAttempts)
	require.Equal(t, []int{AntiDegradeConcurrencyCap, AntiDegradeConcurrencyCap}, upstream.concurrencies)
	require.Equal(t, []string{account.Proxy.URL(), account.Proxy.URL()}, upstream.proxies)
	require.Empty(t, repo.failures)
	require.Equal(t, []int64{91}, repo.successes)
}
