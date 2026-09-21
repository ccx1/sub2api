//go:build unit

package handler

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedImportCodexExportMarkersRemainCompatible(t *testing.T) {
	for _, content := range []string{
		`{"type":"codex","access_token":"access","refresh_token":"refresh"}`,
		`{"type":"codex","tokens":{"access_token":"access","refresh_token":"refresh"}}`,
		`{"auth_mode":"chatgpt","tokens":{"access_token":"access","refresh_token":"refresh"}}`,
		`{"platform":"openai","type":"oauth","accessToken":"access"}`,
		`{"type":"chatgpt","access_token":"access","extra":{"shared_pool_owner_id":10},"proxy_id":1,"group_ids":[2],"owner_user_id":8}`,
	} {
		entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: content}}, Defaults: importTestDefaults()})
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.Empty(t, entries[0].item.Message)
		require.Equal(t, "openai", entries[0].input.Platform)
		require.Equal(t, "oauth", entries[0].input.Type)
		require.Equal(t, "access", entries[0].input.Credentials["access_token"])
		require.Nil(t, entries[0].input.ProxyURL)
	}
}

func TestSharedImportCodexRejectsIncompatibleExplicitPlatformAndType(t *testing.T) {
	for _, content := range []string{
		`{"platform":"gemini","access_token":"secret"}`,
		`{"platform":"anthropic","type":"oauth","tokens":{"access_token":"secret"}}`,
		`{"platform":12,"access_token":"secret"}`,
		`{"type":"apikey","accessToken":"secret"}`,
		`{"type":"sub2api-data","access_token":"secret"}`,
		`{"credentials":null,"access_token":"secret"}`,
	} {
		entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: content}}, Defaults: importTestDefaults()})
		require.NoError(t, err)
		require.NotEmpty(t, entries[0].item.Message)
		require.NotContains(t, entries[0].item.Message, "secret")
		require.Nil(t, entries[0].input.Credentials)
	}
}

func TestSharedImportAssignsDefaultAndSourceNamesToActualInput(t *testing.T) {
	content := `[{"credentials":{"access_token":"one"}},{"type":"codex","tokens":{"access_token":"two"}},{"name":"来源名称","access_token":"three"},{"user":{"name":"session名称"},"accessToken":"four"}]`
	entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: content}}, Defaults: importTestDefaults()})
	require.NoError(t, err)
	for i, name := range []string{"表单名称 #1", "表单名称 #2", "来源名称", "session名称"} {
		require.Equal(t, name, entries[i].input.Name)
		require.Equal(t, name, entries[i].item.Name)
	}
	for _, name := range []string{"", "自定义名称"} {
		defaults := importTestDefaults()
		defaults.Name = name
		entries, err = parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: `{"credentials":{"access_token":"one"}}`}}, Defaults: defaults})
		require.NoError(t, err)
		want := "自定义名称"
		if name == "" {
			want = "导入账号 #1"
		}
		require.Equal(t, want, entries[0].input.Name)
		require.Equal(t, want, entries[0].item.Name)
	}
}
