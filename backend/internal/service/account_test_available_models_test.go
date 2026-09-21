package service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

func TestAvailableAccountModelsSharesUpstreamCatalog(t *testing.T) {
	for _, body := range []string{`{"data":[{"id":"upstream-only","display_name":"Upstream Model"}]}`, `{"data":[]}`} {
		t.Run(body, func(t *testing.T) {
			gateway := newCodexModelsAPIKeyTestService(&codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
				return ordinaryModelsUpstreamResponse(body), nil
			}})
			svc := &AccountTestService{openaiGatewayService: gateway}
			models := svc.GetAvailableModels(context.Background(), newCodexModelsAPIKeyTestAccount("https://models.example/v1")).([]openai.Model)
			if body == `{"data":[]}` {
				require.Empty(t, models, "an empty upstream catalog must stay authoritative")
			} else {
				require.Len(t, models, 1)
				require.Equal(t, "upstream-only", models[0].ID)
				require.Equal(t, "Upstream Model", models[0].DisplayName)
			}
		})
	}
}

func TestAvailableAccountModelsFallsBackOnDiscoveryFailure(t *testing.T) {
	gateway := newCodexModelsAPIKeyTestService(&codexModelsHTTPUpstreamStub{do: func(_ *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		return nil, errors.New("upstream unavailable")
	}})
	svc := &AccountTestService{openaiGatewayService: gateway}
	account := newCodexModelsAPIKeyTestAccount("https://models.example/v1")
	account.Credentials["model_mapping"] = map[string]any{"my-model": "private-upstream-model"}
	models := svc.GetAvailableModels(context.Background(), account).([]openai.Model)
	require.Len(t, models, 1)
	require.Equal(t, "my-model", models[0].ID)
	require.Equal(t, "my-model", models[0].DisplayName)
}

func TestAvailableAccountModelsPreservesPlatformCatalogs(t *testing.T) {
	var svc *AccountTestService
	for _, tc := range []struct {
		name     string
		account  Account
		expected any
	}{
		{"claude oauth", Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}, claude.DefaultModels},
		{"claude api key", Account{Platform: PlatformAnthropic, Type: AccountTypeAPIKey}, claude.DefaultModels},
		{"gemini oauth", Account{Platform: PlatformGemini, Type: AccountTypeOAuth}, geminicli.DefaultModels},
		{"gemini google one", Account{Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"oauth_type": "google_one"}}, geminicli.GoogleOneModels},
		{"antigravity", Account{Platform: PlatformAntigravity, Type: AccountTypeOAuth}, antigravity.DefaultModels()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, svc.GetAvailableModels(context.Background(), &tc.account))
		})
	}
}
