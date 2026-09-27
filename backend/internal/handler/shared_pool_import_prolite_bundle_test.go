//go:build unit

package handler

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedImportProliteBundlePreservesTierAndFormConsent(t *testing.T) {
	const content = `{"type":"sub2api-data","version":1,"proxies":[],"accounts":[{"name":"import@example.test","platform":"openai","type":"oauth","concurrency":3,"priority":50,"credentials":{"access_token":"fixture-token","plan_type":"prolite"},"extra":{"auth_provider":"google"}}]}`
	for _, consent := range []bool{false, true} {
		t.Run(map[bool]string{false: "without consent", true: "with consent"}[consent], func(t *testing.T) {
			defaults := sharedImportDefaults{Concurrency: 2, Enabled: consent, DispatchConsent: consent}
			entries, err := parseSharedImport(sharedImportRequest{
				Sources: []sharedImportSource{{Name: "fixture.json", Content: content}}, Defaults: defaults,
			})
			require.NoError(t, err)
			require.Len(t, entries, 1)
			require.Empty(t, entries[0].item.Message)
			calls := 0
			var captured service.SharedPoolAccountInput
			var capturedOwner int64
			result, err := executeSharedImport(context.Background(), 7, entries, func(_ context.Context, owner int64, input service.SharedPoolAccountInput) (*service.SharedPoolAccountView, error) {
				calls++
				captured, capturedOwner = input, owner
				return &service.SharedPoolAccountView{ID: 41, Name: input.Name}, nil
			})
			require.NoError(t, err)
			require.Equal(t, int64(7), capturedOwner)
			require.Equal(t, "openai", captured.Platform)
			require.Equal(t, "oauth", captured.Type)
			require.Equal(t, "prolite", captured.Credentials["plan_type"])
			require.Equal(t, 2, captured.Concurrency, "共享导入采用表单并发，不复制导出账号配置")
			require.Equal(t, consent, captured.DispatchConsent, "JSON 不能替代用户的调度授权")
			require.Equal(t, consent, captured.Enabled)
			require.Equal(t, 1, calls)
			require.Equal(t, 1, result.Created)
			require.Zero(t, result.Failed)
		})
	}
}
