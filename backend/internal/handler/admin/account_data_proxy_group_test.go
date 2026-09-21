package admin

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestImportDataRejectsSourceRandomProxyGroupWithoutCreatingAccount(t *testing.T) {
	for _, policy := range []string{"reject", "disable", "direct"} {
		t.Run(policy, func(t *testing.T) {
			router, admin := setupAccountDataRouter()
			groupID := int64(7)
			admin.proxies = []service.Proxy{{ID: 9, Name: "target-proxy", GroupID: &groupID, GroupName: "different-target-group"}}
			extra := map[string]any{service.ProxyModeExtraKey: " RaNdOm ", service.RandomProxyPoolScopeExtraKey: " GrOuP ",
				service.RandomProxyGroupIDExtraKey: groupID, service.RandomProxyEmptyPoolPolicyExtraKey: policy}
			before := maps.Clone(extra)
			request := DataImportRequest{Data: DataPayload{Proxies: []DataProxy{}, Accounts: []DataAccount{{
				Name: "source-group-account", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test"}, Extra: extra,
			}}}}
			body, err := json.Marshal(request)
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			require.Equal(t, http.StatusOK, recorder.Code)
			var result struct {
				Data DataImportResult `json:"data"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &result))
			require.Equal(t, 1, result.Data.AccountFailed)
			require.Zero(t, result.Data.AccountCreated)
			require.Len(t, result.Data.Errors, 1)
			require.Equal(t, "组内随机账号导入需要在目标实例重新选择代理组，不能复用源实例分组 ID", result.Data.Errors[0].Message)
			require.Empty(t, admin.createdAccounts)
			require.Empty(t, admin.createdProxies)
			require.Equal(t, before, extra)
			require.Equal(t, &groupID, admin.proxies[0].GroupID)
		})
	}
}

func TestImportDataKeepsFixedGlobalAndSelectedProxyBehavior(t *testing.T) {
	for _, scope := range []string{"fixed", service.RandomProxyPoolAll, service.RandomProxyPoolSelected} {
		t.Run(scope, func(t *testing.T) {
			router, admin := setupAccountDataRouter()
			extra := map[string]any{service.ProxyModeExtraKey: service.ProxyModeRandom, service.RandomProxyPoolScopeExtraKey: scope}
			if scope == "fixed" {
				extra[service.ProxyModeExtraKey] = "fixed"
				extra[service.RandomProxyPoolScopeExtraKey] = service.RandomProxyPoolGroup
				extra[service.RandomProxyGroupIDExtraKey] = int64(7)
			} else if scope == service.RandomProxyPoolSelected {
				extra[service.RandomProxyPoolIDsExtraKey] = []int64{9}
			}
			payload := DataImportRequest{Data: DataPayload{Proxies: []DataProxy{}, Accounts: []DataAccount{{
				Name: "compatible-account", Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
				Credentials: map[string]any{"api_key": "test"}, Extra: extra,
			}}}}
			body, err := json.Marshal(payload)
			require.NoError(t, err)
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			require.Equal(t, http.StatusOK, recorder.Code)
			require.Len(t, admin.createdAccounts, 1)
			require.Equal(t, scope == "fixed", !(&service.Account{Extra: admin.createdAccounts[0].Extra}).IsRandomProxy())
		})
	}
}
