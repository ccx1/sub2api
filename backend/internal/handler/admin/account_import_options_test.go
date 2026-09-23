package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/"+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return store, rec
}
