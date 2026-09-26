//go:build unit

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newSharedDefaultsHandler(t *testing.T, raw string) (*SharedPoolHandler, *sharedImportRepositoryStub) {
	t.Helper()
	repo := &sharedImportRepositoryStub{accounts: map[int64]*service.Account{}, owners: map[int64]int64{}, seen: map[string]bool{}}
	settingsRepo := &sharedTicketPolicyRepository{values: map[string]string{service.SettingKeyAccountImportSettings: raw}}
	settings := service.NewSettingService(settingsRepo, &config.Config{})
	pool := service.NewSharedPoolService(repo, repo, sharedImportGroupsStub{}, nil, service.NewAntiDegradeService(nil, settingsRepo), sharedImportEarningsStub{}, nil)
	return NewSharedPoolHandler(pool, nil, nil, nil, nil, nil, nil, nil, nil, nil, settings), repo
}

func sharedDefaultsContext(t *testing.T, body any) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 901})
	c.Request = httptest.NewRequest(http.MethodPost, "/shared-pool/accounts", strings.NewReader(string(encoded)))
	return c, w
}

func runSharedDefaultsCreate(t *testing.T, h *SharedPoolHandler, mode string, input map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	var body any = input
	if mode == "import" {
		content, err := json.Marshal(input)
		require.NoError(t, err)
		defaults := make(map[string]any)
		for key, value := range input {
			if key != "credentials" {
				defaults[key] = value
			}
		}
		body = map[string]any{"sources": []map[string]string{{"content": string(content)}}, "defaults": defaults}
	}
	c, w := sharedDefaultsContext(t, body)
	if mode == "import" {
		h.ImportAccounts(c)
	} else {
		h.Create(c)
	}
	return w
}

func sharedDefaultsInput() map[string]any {
	return map[string]any{"name": "shared defaults", "platform": "openai", "type": "oauth", "concurrency": 2,
		"enabled": true, "dispatch_consent": true, "credentials": map[string]any{"access_token": "test-token", "plan_type": "team"}}
}

func TestSharedImportDefaultsApplyOnlyRequestedFamilies(t *testing.T) {
	for _, mode := range []string{"create", "import"} {
		t.Run(mode, func(t *testing.T) {
			h, repo := newSharedDefaultsHandler(t, `{"enabled":true,"protection_enabled":false,"codex_ticket_enabled":false,"excel_bps_enabled":true,"excel_bps_options":{"models":["gpt-6-sol"],"auto_disable_on_403":true,"cache_creation_as_input":true},"proxy_mode":"direct","extra":{"proxy_region_mode":"manual","proxy_region_country":"US"}}`)
			w := runSharedDefaultsCreate(t, h, mode, sharedDefaultsInput())
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Len(t, repo.accounts, 1, w.Body.String())
			a := repo.accounts[1]
			require.False(t, a.AntiDegradationEnabled())
			require.False(t, service.OpenAICodexTicketAccountEnabled(a))
			require.True(t, a.IsExcelBPSEnabled())
			options := service.ExcelBPSOptionsFromAccount(a)
			require.NotNil(t, options)
			require.Equal(t, []string{"gpt-6-sol"}, *options.Models)
			require.True(t, options.AutoDisableOn403)
			require.True(t, options.CacheCreationAsInput)
			require.True(t, a.IsRandomProxy(), "global direct routing must not replace shared proxy rules")
			require.Equal(t, "reject", a.Extra[service.RandomProxyEmptyPoolPolicyExtraKey])
			require.NotContains(t, a.Extra, "proxy_region_country")
			require.Equal(t, 17, a.Priority)
			require.Equal(t, 2, a.Concurrency)
			require.Equal(t, int64(901), repo.owners[1])
		})
	}
}

func TestSharedImportDefaultsPreserveExplicitProtectionAndBPSFamily(t *testing.T) {
	for _, mode := range []string{"create", "import"} {
		for _, bps := range []bool{false, true} {
			t.Run(mode+"/bps="+map[bool]string{false: "off", true: "on"}[bps], func(t *testing.T) {
				h, repo := newSharedDefaultsHandler(t, `{"enabled":true,"protection_enabled":true,"codex_ticket_enabled":true,"excel_bps_enabled":true,"excel_bps_options":{"models":["gpt-6-sol"],"auto_disable_on_403":true,"cache_creation_as_input":true}}`)
				in := sharedDefaultsInput()
				in["protection_enabled"], in["codex_ticket_enabled"], in["excel_bps_enabled"] = false, false, bps
				w := runSharedDefaultsCreate(t, h, mode, in)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				require.Len(t, repo.accounts, 1, w.Body.String())
				a := repo.accounts[1]
				require.False(t, a.AntiDegradationEnabled())
				require.Equal(t, !bps, service.OpenAICodexTicketAccountEnabled(a))
				require.Equal(t, true, a.Extra[service.OpenAICodexTicketEnabledExtraKey])
				require.Equal(t, bps, a.IsExcelBPSEnabled())
				if bps {
					require.Equal(t, &service.ExcelBPSOptions{}, service.ExcelBPSOptionsFromAccount(a))
				}
			})
		}
	}
}

func TestSharedImportDefaultsConfigIsAllowlistedAndHonorsMasterSwitch(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		raw, err := json.Marshal(service.AccountImportSettings{Enabled: enabled, ProtectionEnabled: false, CodexTicketEnabled: false, ExcelBPSEnabled: true, ProxyMode: "direct", Extra: map[string]any{}})
		require.NoError(t, err)
		h, _ := newSharedDefaultsHandler(t, string(raw))
		c, w := sharedDefaultsContext(t, nil)
		h.Config(c)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var response struct {
			Data map[string]json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		if !enabled {
			require.NotContains(t, response.Data, "import_defaults")
			continue
		}
		var defaults map[string]any
		require.NoError(t, json.Unmarshal(response.Data["import_defaults"], &defaults))
		require.Len(t, defaults, 4)
		require.Equal(t, false, defaults["protection_enabled"])
		require.Equal(t, false, defaults["codex_ticket_enabled"])
		require.Equal(t, true, defaults["excel_bps_enabled"])
		require.Contains(t, defaults, "excel_bps_options")
	}
}

func TestSharedImportDefaultsInvalidSettingsPreventWrites(t *testing.T) {
	for _, mode := range []string{"create", "import"} {
		h, repo := newSharedDefaultsHandler(t, `{invalid`)
		w := runSharedDefaultsCreate(t, h, mode, sharedDefaultsInput())
		require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
		require.Empty(t, repo.accounts)
	}
}

func TestSharedImportDefaultsProUsesConfiguredTicketSwitch(t *testing.T) {
	for _, mode := range []string{"create", "import"} {
		for _, tier := range []string{"pro", "prolite"} {
			for _, enabled := range []bool{false, true} {
				t.Run(mode+"/"+tier+"/"+map[bool]string{false: "off", true: "on"}[enabled], func(t *testing.T) {
					raw, err := json.Marshal(service.AccountImportSettings{Enabled: true, ProtectionEnabled: false, CodexTicketEnabled: enabled, ProxyMode: "direct", Extra: map[string]any{}})
					require.NoError(t, err)
					h, repo := newSharedDefaultsHandler(t, string(raw))
					in := sharedDefaultsInput()
					in["credentials"].(map[string]any)["plan_type"] = tier
					w := runSharedDefaultsCreate(t, h, mode, in)
					require.Equal(t, http.StatusOK, w.Code, w.Body.String())
					require.Len(t, repo.accounts, 1, w.Body.String())
					require.Equal(t, enabled, service.OpenAICodexTicketAccountEnabled(repo.accounts[1]))
					require.Equal(t, enabled, repo.accounts[1].Extra[service.OpenAICodexTicketEnabledExtraKey])
				})
			}
		}
	}
}

func TestSharedImportDefaultsUnsupportedAccountsKeepSharedRouting(t *testing.T) {
	for _, mode := range []string{"create", "import"} {
		h, repo := newSharedDefaultsHandler(t, `{"enabled":true,"protection_enabled":true,"codex_ticket_enabled":true,"excel_bps_enabled":true,"proxy_mode":"direct"}`)
		in := sharedDefaultsInput()
		in["platform"] = "gemini"
		w := runSharedDefaultsCreate(t, h, mode, in)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Len(t, repo.accounts, 1, w.Body.String())
		a := repo.accounts[1]
		require.True(t, a.AntiDegradationEnabled())
		require.False(t, a.IsExcelBPSEnabled())
		require.NotContains(t, a.Extra, service.OpenAICodexTicketEnabledExtraKey)
		require.Equal(t, []int64{8, 10}, a.GroupIDs)
		require.Equal(t, 17, a.Priority)
		require.True(t, a.IsRandomProxy())
	}
}

func TestSharedImportDefaultsDisabledKeepExistingBehavior(t *testing.T) {
	for _, mode := range []string{"create", "import"} {
		h, repo := newSharedDefaultsHandler(t, `{"enabled":false,"protection_enabled":false,"codex_ticket_enabled":false,"excel_bps_enabled":true}`)
		in := sharedDefaultsInput()
		in["protection_enabled"] = true
		in["codex_ticket_enabled"] = false
		w := runSharedDefaultsCreate(t, h, mode, in)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Len(t, repo.accounts, 1, w.Body.String())
		a := repo.accounts[1]
		require.True(t, a.AntiDegradationEnabled())
		require.True(t, service.OpenAICodexTicketAccountEnabled(a))
		require.False(t, a.IsExcelBPSEnabled())
	}
}

func TestSharedImportDefaultsIgnoreUserTicketOverrides(t *testing.T) {
	for _, mode := range []string{"create", "import"} {
		for _, enabled := range []bool{false, true} {
			raw, err := json.Marshal(service.AccountImportSettings{Enabled: true, ProtectionEnabled: false, CodexTicketEnabled: enabled, ProxyMode: "direct", Extra: map[string]any{}})
			require.NoError(t, err)
			h, repo := newSharedDefaultsHandler(t, string(raw))
			in := sharedDefaultsInput()
			in["codex_ticket_enabled"] = !enabled
			w := runSharedDefaultsCreate(t, h, mode, in)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Len(t, repo.accounts, 1, w.Body.String())
			require.Equal(t, enabled, service.OpenAICodexTicketAccountEnabled(repo.accounts[1]))
			require.Equal(t, enabled, repo.accounts[1].Extra[service.OpenAICodexTicketEnabledExtraKey])
		}
	}
}

func TestSharedImportDefaultsCloneBPSOptionsPerAccount(t *testing.T) {
	models := []string{"gpt-6-sol"}
	defaults := &sharedAccountImportDefaults{ExcelBPSEnabled: true, ExcelBPSOptions: service.ExcelBPSOptions{Models: &models}}
	a := service.SharedPoolAccountInput{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth}
	b := a
	applySharedAccountImportDefaults(&a, defaults, sharedImportDefaults{})
	applySharedAccountImportDefaults(&b, defaults, sharedImportDefaults{})
	(*a.ExcelBPSOptions.Models)[0] = "changed"
	require.Equal(t, "gpt-6-sol", (*b.ExcelBPSOptions.Models)[0])
	require.Equal(t, "gpt-6-sol", models[0])
}
