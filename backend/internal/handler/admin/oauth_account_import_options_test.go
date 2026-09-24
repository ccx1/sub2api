//go:build unit

package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type importOptionsOpenAIClient struct{}

func (importOptionsOpenAIClient) ExchangeCode(context.Context, string, string, string, string, string) (*openai.TokenResponse, error) {
	return &openai.TokenResponse{AccessToken: "access-token", RefreshToken: "refresh-token", ExpiresIn: 3600}, nil
}

func (importOptionsOpenAIClient) RefreshToken(context.Context, string, string) (*openai.TokenResponse, error) {
	panic("unexpected refresh")
}

func (importOptionsOpenAIClient) RefreshTokenWithClientID(context.Context, string, string, string) (*openai.TokenResponse, error) {
	panic("unexpected refresh")
}

type importOptionsPATTransport struct{}

func (importOptionsPATTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{"email":"user@example.com","chatgpt_user_id":"user-1",` +
			`"chatgpt_account_id":"account-1","chatgpt_plan_type":"plus","chatgpt_account_is_fedramp":false}`)),
		Request: req,
	}, nil
}

func useImportOptionsPATTransport(t *testing.T) {
	t.Helper()
	client, err := httpclient.GetClient(httpclient.Options{Timeout: 20 * time.Second, ResponseHeaderTimeout: 15 * time.Second})
	require.NoError(t, err)
	original := client.Transport
	client.Transport = importOptionsPATTransport{}
	t.Cleanup(func() { client.Transport = original })
}

func oauthImportEndpoint(t *testing.T, kind string, admin *stubAdminService) (gin.HandlerFunc, map[string]any) {
	t.Helper()
	if kind == "openai" || kind == "pat" {
		svc := service.NewOpenAIOAuthService(nil, importOptionsOpenAIClient{})
		t.Cleanup(svc.Stop)
		handler := NewOpenAIOAuthHandler(svc, admin, nil, nil)
		if kind == "pat" {
			return handler.CreateAccountFromCodexPAT, map[string]any{"access_token": "at-test-token"}
		}
		session, err := svc.GenerateAuthURL(context.Background(), nil, "", service.PlatformOpenAI)
		require.NoError(t, err)
		authURL, err := url.Parse(session.AuthURL)
		require.NoError(t, err)
		return handler.CreateAccountFromOAuth, map[string]any{
			"session_id": session.SessionID, "state": authURL.Query().Get("state"), "code": "test-code",
		}
	}
	svc := service.NewGrokOAuthService(nil, grokImportOAuthClientStub{})
	t.Cleanup(svc.Stop)
	handler := NewGrokOAuthHandler(svc, admin, nil, nil)
	handler.importProber = nil
	if kind == "sso" {
		return handler.CreateAccountsFromSSO, map[string]any{"sso_token": "test-sso-token"}
	}
	session, err := svc.GenerateAuthURL(context.Background(), nil, "")
	require.NoError(t, err)
	return handler.CreateAccountFromOAuth, map[string]any{
		"session_id": session.SessionID, "state": session.State, "code": "test-code",
	}
}

func serveOAuthImport(t *testing.T, handler gin.HandlerFunc, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	router := gin.New()
	router.POST("/create", handler)
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, req)
	return recorder
}

func TestOAuthCreationPassesIndependentImportOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useImportOptionsPATTransport(t)
	options := []struct {
		name       string
		protection *bool
		ticket     *bool
		defaults   *bool
	}{
		{"omitted", nil, nil, nil},
		{"off", new(false), new(false), new(false)},
		{"on", new(true), new(true), new(true)},
		{"independent", new(true), new(false), nil},
	}
	for _, kind := range []string{"openai", "pat", "grok", "sso"} {
		for _, option := range options {
			t.Run(kind+"/"+option.name, func(t *testing.T) {
				admin := newStubAdminService()
				handler, payload := oauthImportEndpoint(t, kind, admin)
				if option.protection != nil {
					payload["protection_enabled"] = *option.protection
					payload["codex_ticket_enabled"] = *option.ticket
				}
				if option.defaults != nil {
					payload["use_import_defaults"] = *option.defaults
				}
				rec := serveOAuthImport(t, handler, payload)
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Len(t, admin.createdAccounts, 1, rec.Body.String())
				input := admin.createdAccounts[0]
				require.Equal(t, option.protection, input.ProtectionEnabled)
				require.Equal(t, option.ticket, input.CodexTicketEnabled)
				require.Equal(t, option.defaults != nil && !*option.defaults, input.SkipImportDefaults)
			})
		}
	}
}

func TestOAuthCreationExplicitDirectProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	useImportOptionsPATTransport(t)
	for _, kind := range []string{"openai", "pat", "grok", "sso"} {
		t.Run(kind, func(t *testing.T) {
			admin := newStubAdminService()
			handler, payload := oauthImportEndpoint(t, kind, admin)
			payload["proxy_id"] = 0
			rec := serveOAuthImport(t, handler, payload)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Len(t, admin.createdAccounts, 1, rec.Body.String())
			require.Equal(t, new(int64(0)), admin.createdAccounts[0].ProxyID)
		})
	}
}

func TestOAuthCreationRejectsInvalidImportOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, kind := range []string{"openai", "pat", "grok", "sso"} {
		for _, field := range []string{"protection_enabled", "codex_ticket_enabled", "use_import_defaults", "proxy_id"} {
			t.Run(kind+"/"+field, func(t *testing.T) {
				admin := newStubAdminService()
				handler, payload := oauthImportEndpoint(t, kind, admin)
				payload[field] = "false"
				if field == "proxy_id" {
					payload[field] = -1
				}
				rec := serveOAuthImport(t, handler, payload)
				require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
				require.Empty(t, admin.createdAccounts)
			})
		}
	}
}
