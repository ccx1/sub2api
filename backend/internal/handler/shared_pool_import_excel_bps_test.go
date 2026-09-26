//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedImportExcelBPSOnlyAppliesToOpenAIOAuth(t *testing.T) {
	content := `[
		{"name":"openai","platform":"openai","type":"oauth","credentials":{"access_token":"openai-token"}},
		{"name":"gemini","platform":"gemini","type":"oauth","credentials":{"access_token":"gemini-token"}},
		{"name":"api-key","platform":"openai","type":"apikey","credentials":{"api_key":"api-token"}},
		{"tokens":{"access_token":"codex-token"}}
	]`
	for _, raw := range []string{`{}`, `{"excel_bps_enabled":false}`, `{"excel_bps_enabled":true}`} {
		t.Run(raw, func(t *testing.T) {
			defaults := importTestDefaults()
			defaults.Enabled, defaults.ProtectionEnabled = false, new(false)
			require.NoError(t, json.Unmarshal([]byte(raw), &defaults))
			entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: content}}, Defaults: defaults})
			require.NoError(t, err)
			require.Len(t, entries, 4)
			for _, index := range []int{0, 3} {
				require.Equal(t, defaults.ExcelBPSEnabled, entries[index].input.ExcelBPSEnabled)
			}
			for _, index := range []int{1, 2} {
				require.Nil(t, entries[index].input.ExcelBPSEnabled)
			}
			r := &sharedImportRepositoryStub{accounts: map[int64]*service.Account{}, owners: map[int64]int64{}, seen: map[string]bool{}}
			pool := service.NewSharedPoolService(r, r, nil, nil, nil, sharedImportEarningsStub{}, nil)
			result, err := executeSharedImport(context.Background(), 901, entries, pool.Create)
			require.NoError(t, err)
			require.Equal(t, 4, result.Created)
			want := defaults.ExcelBPSEnabled != nil && *defaults.ExcelBPSEnabled
			for _, account := range r.accounts {
				require.Equal(t, want && account.IsOpenAIOAuthLike(), account.IsExcelBPSEnabled())
			}
		})
	}
}

func TestSharedImportExcelBPSOptionsCopiedPerOpenAIOAuthEntry(t *testing.T) {
	content := `[
		{"name":"openai","platform":"openai","type":"oauth","credentials":{"access_token":"openai-token"}},
		{"name":"api-key","platform":"openai","type":"apikey","credentials":{"api_key":"api-token"}},
		{"tokens":{"access_token":"codex-token"}}
	]`
	defaults := importTestDefaults()
	defaults.Enabled, defaults.ProtectionEnabled = false, new(false)
	require.NoError(t, json.Unmarshal([]byte(`{"excel_bps_enabled":true,"excel_bps_options":{"models":["gpt-6-astra"],"auto_disable_on_403":true,"cache_creation_as_input":false}}`), &defaults))
	entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: content}}, Defaults: defaults})
	require.NoError(t, err)
	require.Len(t, entries, 3)
	require.Nil(t, entries[1].input.ExcelBPSOptions)
	for _, index := range []int{0, 2} {
		options := entries[index].input.ExcelBPSOptions
		require.NotNil(t, options)
		require.Equal(t, []string{"gpt-6-astra"}, *options.Models)
		require.True(t, options.AutoDisableOn403)
	}
	(*entries[0].input.ExcelBPSOptions.Models)[0] = "mutated"
	require.Equal(t, "gpt-6-astra", (*entries[2].input.ExcelBPSOptions.Models)[0], "每个条目的模型列表需独立")

	r := &sharedImportRepositoryStub{accounts: map[int64]*service.Account{}, owners: map[int64]int64{}, seen: map[string]bool{}}
	pool := service.NewSharedPoolService(r, r, nil, nil, nil, sharedImportEarningsStub{}, nil)
	(*entries[0].input.ExcelBPSOptions.Models)[0] = "gpt-6-astra"
	result, err := executeSharedImport(context.Background(), 901, entries, pool.Create)
	require.NoError(t, err)
	require.Equal(t, 3, result.Created)
	for _, account := range r.accounts {
		if !account.IsOpenAIOAuthLike() {
			require.False(t, account.IsExcelBPSEnabled())
			continue
		}
		require.True(t, account.IsExcelBPSEnabled())
		require.True(t, account.IsExcelBPSAutoDisableOn403Enabled())
		require.False(t, account.IsExcelBPSCacheCreationAsInputEnabled())
		require.Equal(t, []string{"gpt-6-astra"}, account.Extra["openai_excel_bps_models"])
	}
}
