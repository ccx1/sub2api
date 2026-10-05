//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedPrismImportPrecedenceAndOwnerIsolation(t *testing.T) {
	global := &sharedAccountImportDefaults{PrismBrowserEnabled: true, PrismBrowserModels: []string{"gpt-6-luna"}}
	for _, tc := range []struct {
		name, extra, form string
		enabled           bool
		models            []string
	}{
		{"global", `{}`, `{}`, true, []string{"gpt-6-luna"}},
		{"form", `{}`, `{"prism_browser_enabled":true,"prism_browser_models":["gpt-5.6-sol"]}`, true, []string{"gpt-5.6-sol"}},
		{"file", `{"openai_prism_browser":true,"openai_prism_browser_models":["gpt-6.1-sol"],"shared_pool_owner_id":77,"role":"admin"}`, `{"prism_browser_enabled":false}`, true, []string{"gpt-6.1-sol"}},
		{"file off", `{"openai_prism_browser":false,"openai_prism_browser_models":["gpt-6.1-sol"]}`, `{"prism_browser_enabled":true}`, false, []string{}},
		{"empty scope", `{"openai_prism_browser":true,"openai_prism_browser_models":[]}`, `{}`, true, []string{}},
		{"legacy enabled", `{"openai_prism_browser":true}`, `{}`, true, service.PrismBrowserSupportedModels()},
		{"orphan scope", `{"openai_prism_browser_models":["gpt-6.1-sol"]}`, `{}`, false, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defaults := importTestDefaults()
			defaults.Enabled, defaults.ProtectionEnabled = false, new(false)
			require.NoError(t, json.Unmarshal([]byte(tc.form), &defaults))
			content := `{"platform":"openai","type":"oauth","credentials":{"access_token":"fixture"},"extra":` + tc.extra + `}`
			entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: content}}, Defaults: defaults})
			require.NoError(t, err)
			applySharedAccountImportDefaults(&entries[0].input, global, defaults)
			repo := &sharedImportRepositoryStub{accounts: map[int64]*service.Account{}, owners: map[int64]int64{}, seen: map[string]bool{}}
			pool := service.NewSharedPoolService(repo, repo, nil, nil, nil, sharedImportEarningsStub{}, nil)
			result, err := executeSharedImport(t.Context(), 901, entries, pool.Create)
			require.NoError(t, err)
			require.Equal(t, 1, result.Created, result.Items)
			account := repo.accounts[1]
			require.Equal(t, int64(901), account.Extra[service.SharedPoolOwnerKey])
			require.NotContains(t, account.Extra, "role")
			require.Equal(t, tc.enabled, account.Extra[service.PrismBrowserEnabledKey])
			for _, model := range service.PrismBrowserSupportedModels() {
				want := false
				for _, selected := range tc.models {
					want = want || selected == model
				}
				require.Equal(t, want && tc.enabled, account.IsPrismBrowserEnabledForModel(model), model)
			}
		})
	}
}

func TestSharedPrismImportRejectsInvalidAndUnsupportedConfiguration(t *testing.T) {
	for _, tc := range []struct{ platform, extra string }{
		{"openai", `{"openai_prism_browser":"true"}`},
		{"openai", `{"openai_prism_browser":true,"openai_prism_browser_models":["gpt-6-astra"]}`},
		{"gemini", `{"openai_prism_browser":true}`},
	} {
		content := `{"platform":"` + tc.platform + `","type":"oauth","credentials":{"access_token":"fixture"},"extra":` + tc.extra + `}`
		entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: content}}, Defaults: importTestDefaults()})
		require.NoError(t, err)
		require.NotEmpty(t, entries[0].item.Message)
	}
}

func TestSharedPrismGlobalDefaultsApplyToCreateAndImport(t *testing.T) {
	for _, mode := range []string{"create", "import"} {
		t.Run(mode, func(t *testing.T) {
			h, repo := newSharedDefaultsHandler(t, `{"enabled":true,"protection_enabled":false,"codex_ticket_enabled":false,"extra":{"openai_prism_browser":true,"openai_prism_browser_models":["gpt-6.1-sol"]}}`)
			w := runSharedDefaultsCreate(t, h, mode, sharedDefaultsInput())
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Len(t, repo.accounts, 1, w.Body.String())
			require.True(t, repo.accounts[1].IsPrismBrowserEnabledForModel("gpt-6.1-sol"))
			require.False(t, repo.accounts[1].IsPrismBrowserEnabledForModel("gpt-6-luna"))
		})
	}
}
