//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func importTestDefaults() sharedImportDefaults {
	return sharedImportDefaults{Name: "表单名称", Platform: "gemini", Type: "oauth", Concurrency: 3, Enabled: true, DispatchConsent: true, ProtectionEnabled: true}
}

func TestSharedImportCompatibleFormats(t *testing.T) {
	for _, content := range []string{
		`[{"name":"来源账号","platform":"gemini","type":"oauth","credentials":{"access_token":"token"}}]`,
		`{"accounts":[{"name":"来源账号","platform":"gemini","type":"oauth","credentials":{"access_token":"token"}}]}`,
		`{"type":"sub2api-data","version":1,"proxies":[],"accounts":[{"name":"来源账号","platform":"gemini","type":"oauth","credentials":{"access_token":"token"}}]}`,
		`{"type":"sub2api-bundle","version":1,"accounts":[{"name":"来源账号","platform":"gemini","type":"oauth","credentials":{"access_token":"token"}}]}`,
		`{"name":"来源账号","platform":"gemini","type":"oauth","credentials":{"access_token":"token"}}`,
	} {
		entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Name: "one.json", Content: content}}, Defaults: importTestDefaults()})
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.Empty(t, entries[0].item.Message)
		require.Equal(t, "来源账号", entries[0].input.Name)
		require.Equal(t, "gemini", entries[0].input.Platform)
		require.Equal(t, "token", entries[0].input.Credentials["access_token"])
	}
}

func TestSharedImportCodexFormats(t *testing.T) {
	for _, content := range []string{`raw-token`, `{"accessToken":"raw-token"}`, `{"tokens":{"access_token":"raw-token","refresh_token":"refresh"}}`, `["raw-token"]`} {
		entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: content}}, Defaults: importTestDefaults()})
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.Empty(t, entries[0].item.Message)
		require.Equal(t, "openai", entries[0].input.Platform)
		require.Equal(t, "oauth", entries[0].input.Type)
		require.Equal(t, "raw-token", entries[0].input.Credentials["access_token"])
	}
}

func TestSharedImportDropsPrivilegedFieldsAndUsesFormSettings(t *testing.T) {
	content := `{"proxies":[{"proxy_key":"private","host":"127.0.0.1"}],"accounts":[{"name":"account","credentials":{"access_token":"token"},"concurrency":999,"proxy_key":"private","proxy_id":8,"group_ids":[123],"owner_user_id":88,"rate_multiplier":0,"priority":1,"extra":{"shared_pool_owner_id":88},"enabled":false,"protection_enabled":false}]}`
	entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: content}}, Defaults: importTestDefaults()})
	require.NoError(t, err)
	in := entries[0].input
	require.Equal(t, 3, in.Concurrency)
	require.Nil(t, in.ProxyURL)
	require.True(t, in.Enabled)
	require.True(t, in.ProtectionEnabled)
	encoded, err := json.Marshal(in)
	require.NoError(t, err)
	for _, field := range []string{"owner_user_id", "group_ids", "rate_multiplier", "priority", "proxy_id", "extra"} {
		require.NotContains(t, string(encoded), field)
	}
	entries, err = parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: `{"accessToken":"token","group_ids":[123]}`}}, Defaults: importTestDefaults()})
	require.NoError(t, err)
	require.Empty(t, entries[0].item.Message)
	require.Equal(t, "token", entries[0].input.Credentials["access_token"])
}

func TestSharedImportLimitsAndBrokenSourceRejectBeforeCreation(t *testing.T) {
	for _, req := range []sharedImportRequest{
		{}, {Sources: make([]sharedImportSource, 21)},
		{Sources: []sharedImportSource{{Content: strings.Repeat("x", sharedImportMaxContent+1)}}},
		{Sources: []sharedImportSource{{Content: strings.Repeat("token\n", 51)}}},
		{Sources: []sharedImportSource{{Content: `token`}, {Content: `{"accounts":broken}`}}},
		{Sources: []sharedImportSource{{Content: `{"type":"unknown","accounts":[]}`}}},
		{Sources: []sharedImportSource{{Content: `{"version":2,"accounts":[]}`}}},
		{Sources: []sharedImportSource{{Content: `{"arbitrary":"object"}`}}},
	} {
		_, err := parseSharedImport(req)
		require.Error(t, err)
	}
	for _, body := range []string{`{"defaults":{"group_ids":[1]}}`, `{} {}`, strings.Repeat(" ", sharedImportMaxBody) + `{}`} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
		require.False(t, bindSharedImport(c, &sharedImportRequest{}))
	}
	// pool 为 nil，若未先解析全部文件而进行写入，此请求会 panic。
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 42})
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"sources":[{"content":"token"},{"content":"{broken}"}]}`))
	(&SharedPoolHandler{}).ImportAccounts(c)
	require.Equal(t, 400, w.Code)
}

func TestSharedImportBadArrayItemAllowsValidItems(t *testing.T) {
	entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Name: "array.json", Content: `[42,{"unexpected":"value"},{"accessToken":"valid-token"}]`}}, Defaults: importTestDefaults()})
	require.NoError(t, err)
	called := 0
	result, err := executeSharedImport(context.Background(), 800, entries, func(_ context.Context, _ int64, in service.SharedPoolAccountInput) (*service.SharedPoolAccountView, error) {
		called++
		return &service.SharedPoolAccountView{ID: 5, Name: in.Name}, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, called)
	require.Equal(t, 1, result.Created)
	require.Equal(t, 2, result.Failed)
	require.Equal(t, 3, result.Items[2].Index)
	require.Equal(t, "array.json", result.Items[2].Source)
}
