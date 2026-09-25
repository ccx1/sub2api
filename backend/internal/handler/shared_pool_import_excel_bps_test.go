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
			defaults.Enabled, defaults.ProtectionEnabled = false, false
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
