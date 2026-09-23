package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type proxyBatchCreateService struct {
	service.AdminService
	inputs       []*service.CreateProxyInput
	checkedHosts []string
	duplicate    string
}

func (s *proxyBatchCreateService) CheckProxyExists(_ context.Context, host string, _ int, _, _ string) (bool, error) {
	s.checkedHosts = append(s.checkedHosts, host)
	return host == s.duplicate, nil
}

func (s *proxyBatchCreateService) CreateProxy(_ context.Context, input *service.CreateProxyInput) (*service.Proxy, error) {
	s.inputs = append(s.inputs, input)
	return &service.Proxy{ID: int64(len(s.inputs))}, nil
}

func serveProxyBatchCreate(svc *proxyBatchCreateService, body string) *httptest.ResponseRecorder {
	router := gin.New()
	router.POST("/proxies/batch", NewProxyHandler(svc).BatchCreate)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/proxies/batch", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	return w
}

func TestProxyBatchCreatePreservesExpiryAndFallbackForEveryItem(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &proxyBatchCreateService{}
	w := serveProxyBatchCreate(svc, `{"proxies":[
		{"protocol":"http","host":"first.example","port":8080,"expires_at":1800000000,"fallback_mode":"proxy","backup_proxy_id":12,"expiry_warn_days":7},
		{"protocol":"socks5","host":"second.example","port":1080,"expires_at":1800000000,"fallback_mode":"proxy","backup_proxy_id":12,"expiry_warn_days":7},
		{"protocol":"https","host":"third.example","port":443,"expires_at":1900000000,"fallback_mode":"direct","backup_proxy_id":null,"expiry_warn_days":3}
	]}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"created":3`)
	require.Contains(t, w.Body.String(), `"skipped":0`)
	require.Len(t, svc.inputs, 3)
	for _, input := range svc.inputs[:2] {
		require.NotNil(t, input.ExpiresAt)
		require.Equal(t, int64(1800000000), input.ExpiresAt.Unix())
		require.Equal(t, service.FallbackModeProxy, input.FallbackMode)
		require.NotNil(t, input.BackupProxyID)
		require.Equal(t, int64(12), *input.BackupProxyID)
		require.Equal(t, 7, input.ExpiryWarnDays)
	}
	require.Equal(t, int64(1900000000), svc.inputs[2].ExpiresAt.Unix())
	require.Equal(t, service.FallbackModeDirect, svc.inputs[2].FallbackMode)
	require.Nil(t, svc.inputs[2].BackupProxyID)
	require.Equal(t, 3, svc.inputs[2].ExpiryWarnDays)
}

func TestProxyBatchCreatePreservesLegacyDefaultsAndDuplicates(t *testing.T) {
	svc := &proxyBatchCreateService{duplicate: "existing.example"}
	w := serveProxyBatchCreate(svc, `{"proxies":[
		{"protocol":"http","host":" existing.example ","port":8080},
		{"protocol":"socks5h","host":" new.example ","port":1080,"username":" user ","password":" pass "}
	]}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"created":1`)
	require.Contains(t, w.Body.String(), `"skipped":1`)
	require.Equal(t, []string{"existing.example", "new.example"}, svc.checkedHosts)
	require.Len(t, svc.inputs, 1)
	input := svc.inputs[0]
	require.Equal(t, "default", input.Name)
	require.Equal(t, "new.example", input.Host)
	require.Equal(t, "user", input.Username)
	require.Equal(t, "pass", input.Password)
	require.Nil(t, input.ExpiresAt)
	require.Empty(t, input.FallbackMode)
	require.Nil(t, input.BackupProxyID)
	require.Zero(t, input.ExpiryWarnDays)
}

func TestProxyBatchCreateNoExpiryMatchesSingleCreate(t *testing.T) {
	for _, expiry := range []string{"null", "0", "-1"} {
		t.Run(expiry, func(t *testing.T) {
			svc := &proxyBatchCreateService{}
			w := serveProxyBatchCreate(svc, `{"proxies":[{"protocol":"http","host":"proxy.example","port":8080,"expires_at":`+expiry+`,"fallback_mode":"none"}]}`)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Len(t, svc.inputs, 1)
			require.Nil(t, svc.inputs[0].ExpiresAt)
			require.Equal(t, service.FallbackModeNone, svc.inputs[0].FallbackMode)
		})
	}
}

func TestProxyBatchCreateRejectsInvalidPolicyBeforeCreatingAnyProxy(t *testing.T) {
	for _, test := range []struct{ name, fields string }{
		{"unknown fallback", `"fallback_mode":"invalid"`},
		{"missing backup", `"fallback_mode":"proxy"`},
		{"null backup", `"fallback_mode":"proxy","backup_proxy_id":null`},
		{"zero backup", `"fallback_mode":"proxy","backup_proxy_id":0`},
		{"negative backup", `"fallback_mode":"proxy","backup_proxy_id":-1`},
		{"negative warning", `"expiry_warn_days":-1`},
		{"unrepresentable expiry", `"expires_at":253402300800`},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc := &proxyBatchCreateService{}
			w := serveProxyBatchCreate(svc, `{"proxies":[{"protocol":"http","host":"valid.example","port":8080},{"protocol":"http","host":"invalid.example","port":8081,`+test.fields+`}]}`)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			require.Empty(t, svc.inputs)
			require.Empty(t, svc.checkedHosts)
		})
	}
}
