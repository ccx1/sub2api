package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountImportPassesIndependentCreationOptions(t *testing.T) {
	options := []struct {
		name       string
		protection *bool
		ticket     *bool
	}{
		{"omitted", nil, nil},
		{"both_on", new(true), new(true)},
		{"protection_only", new(true), new(false)},
		{"ticket_only", new(false), new(true)},
		{"both_off", new(false), new(false)},
	}
	for _, path := range []string{"data", "batch"} {
		for _, option := range options {
			t.Run(path+"/"+option.name, func(t *testing.T) {
				accounts := []DataAccount{importEditDataAccount("first"), importEditDataAccount("second")}
				payload := map[string]any{"accounts": accounts}
				if path == "data" {
					payload = map[string]any{"data": DataPayload{Accounts: accounts, Proxies: []DataProxy{}}}
				}
				if option.protection != nil {
					payload["protection_enabled"] = *option.protection
					payload["codex_ticket_enabled"] = *option.ticket
				}
				store, rec := serveAccountImportOptions(t, path, payload)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Len(t, store.createdAccounts, 2)
				for _, created := range store.createdAccounts {
					require.Equal(t, option.protection, created.ProtectionEnabled)
					require.Equal(t, option.ticket, created.CodexTicketEnabled)
				}
			})
		}
	}
}

func TestAccountImportRejectsMalformedCreationOptions(t *testing.T) {
	for _, path := range []string{"data", "batch"} {
		for _, field := range []string{"protection_enabled", "codex_ticket_enabled"} {
			t.Run(path+"/"+field, func(t *testing.T) {
				accounts := []DataAccount{importEditDataAccount("first")}
				payload := map[string]any{"accounts": accounts}
				if path == "data" {
					payload = map[string]any{"data": DataPayload{Accounts: accounts, Proxies: []DataProxy{}}}
				}
				payload[field] = "false"
				store, rec := serveAccountImportOptions(t, path, payload)
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Empty(t, store.createdAccounts)
			})
		}
	}
}

func serveAccountImportOptions(t *testing.T, path string, payload map[string]any) (*stubAdminService, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	store := newStubAdminService()
	handler := &AccountHandler{adminService: store}
	router := gin.New()
	router.POST("/data", handler.ImportData)
	router.POST("/batch", handler.BatchCreate)
	router.POST("/create", handler.Create)
	router.POST("/codex", handler.ImportCodexSession)
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/"+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return store, rec
}

func TestAccountImportSingleAndCodexPassOptions(t *testing.T) {
	for _, path := range []string{"create", "codex"} {
		for _, explicit := range []bool{false, true} {
			payload := map[string]any{"name": "imported", "platform": "openai", "type": "oauth", "credentials": map[string]any{"access_token": "test-token"}}
			if path == "codex" {
				payload = map[string]any{"content": buildCodexAccessToken(t, "option-account", "option-user", time.Now().Add(time.Hour))}
			}
			if explicit {
				payload["protection_enabled"] = false
				payload["codex_ticket_enabled"] = false
				payload["use_import_defaults"] = false
				payload["proxy_id"] = 0
			}
			store, rec := serveAccountImportOptions(t, path, payload)
			require.True(t, rec.Code == http.StatusOK || rec.Code == http.StatusCreated, rec.Body.String())
			require.Len(t, store.createdAccounts, 1, "%s: %s", path, rec.Body.String())
			input := store.createdAccounts[0]
			require.Equal(t, explicit, input.SkipImportDefaults)
			if explicit {
				require.Equal(t, new(false), input.ProtectionEnabled)
				require.Equal(t, new(false), input.CodexTicketEnabled)
				require.Equal(t, new(int64(0)), input.ProxyID)
			} else {
				require.Nil(t, input.ProtectionEnabled)
				require.Nil(t, input.CodexTicketEnabled)
				require.Nil(t, input.ProxyID)
			}
		}
	}
}

func TestAccountImportItemOptionsOverrideBatch(t *testing.T) {
	for _, path := range []string{"data", "batch"} {
		first := importEditDataAccount("explicit")
		first.ProtectionEnabled, first.CodexTicketEnabled, first.UseImportDefaults = new(false), new(false), new(true)
		accounts := []DataAccount{first, importEditDataAccount("inherited")}
		payload := map[string]any{"accounts": accounts, "protection_enabled": true, "codex_ticket_enabled": true, "use_import_defaults": false}
		if path == "data" {
			delete(payload, "accounts")
			payload["data"] = DataPayload{Accounts: accounts, Proxies: []DataProxy{}}
		}
		store, rec := serveAccountImportOptions(t, path, payload)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.Len(t, store.createdAccounts, 2)
		require.Equal(t, new(false), store.createdAccounts[0].ProtectionEnabled)
		require.Equal(t, new(false), store.createdAccounts[0].CodexTicketEnabled)
		require.False(t, store.createdAccounts[0].SkipImportDefaults)
		require.Equal(t, new(true), store.createdAccounts[1].ProtectionEnabled)
		require.Equal(t, new(true), store.createdAccounts[1].CodexTicketEnabled)
		require.True(t, store.createdAccounts[1].SkipImportDefaults)
	}
}

func TestCodexImportDefaultsDoNotChangeExistingAccountOptions(t *testing.T) {
	value := buildCodexAccessOnlyImportValue(t, "existing-account", "existing-user")
	extra := map[string]any{service.OpenAICodexTicketEnabledExtraKey: false, "proxy_mode": "random", service.AntiDegradationExtraKey: false}
	store := newCodexImportMemoryAdminService([]service.Account{{
		ID: 17, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Extra: extra,
		Credentials: map[string]any{"chatgpt_account_id": "existing-account", "chatgpt_user_id": "existing-user", "access_token": value["access_token"]},
	}})
	h := &AccountHandler{adminService: store}
	result, err := h.importCodexSessions(context.Background(), CodexSessionImportRequest{}, []codexImportEntry{{Index: 1, Value: value}})
	require.NoError(t, err)
	require.Equal(t, 1, result.Updated)
	require.Empty(t, store.createdAccounts)
	require.Len(t, store.updatedAccounts, 1)
	for key, value := range extra {
		require.Equal(t, value, store.updatedAccounts[0].input.Extra[key], key)
	}
	require.Nil(t, store.updatedAccounts[0].input.ProxyID)
}
