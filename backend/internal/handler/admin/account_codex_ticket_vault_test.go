package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketVaultHandlersRejectMalformedRequests(t *testing.T) {
	fingerprint := strings.Repeat("a", 24)
	for _, tc := range []struct {
		name, method, id, body string
		unavailable            bool
		status                 int
		message                string
	}{
		{"get invalid ID", http.MethodGet, "bad", "", false, 400, "Invalid account ID"},
		{"get unavailable", http.MethodGet, "1", "", true, 503, "unavailable"},
		{"get non OAuth", http.MethodGet, "1", "", false, 400, "non-shadow OpenAI OAuth"},
		{"revoke invalid ID", http.MethodPost, "0", `{}`, false, 400, "Invalid account ID"},
		{"revoke unavailable", http.MethodPost, "1", `{}`, true, 503, "unavailable"},
		{"revoke unknown field", http.MethodPost, "1", `{"model":"m","all":true,"state":"x"}`, false, 400, "Invalid ticket vault request"},
		{"revoke trailing JSON", http.MethodPost, "1", `{"model":"m","all":true} {}`, false, 400, "Invalid ticket vault request"},
		{"revoke too large", http.MethodPost, "1", `{"model":"` + strings.Repeat("x", 9<<10) + `"}`, false, 400, "Invalid ticket vault request"},
		{"revoke empty body", http.MethodPost, "1", "", false, 400, "Invalid ticket vault request"},
		{"revoke missing target", http.MethodPost, "1", `{"model":"m"}`, false, 400, "exactly one of fingerprint or all"},
		{"revoke both targets", http.MethodPost, "1", `{"model":"m","all":true,"fingerprint":"` + fingerprint + `"}`, false, 400, "exactly one of fingerprint or all"},
		{"revoke bad fingerprint", http.MethodPost, "1", `{"model":"m","fingerprint":"zz"}`, false, 400, "invalid fingerprint"},
		{"revoke non OAuth", http.MethodPost, "1", `{"model":"m","fingerprint":"` + fingerprint + `"}`, false, 400, "non-shadow OpenAI OAuth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			h := &AccountHandler{}
			if !tc.unavailable {
				h.codexTicketRetry = &service.OpenAIGatewayService{}
			}
			router := gin.New()
			router.GET("/accounts/:id/codex-ticket/vault", h.GetCodexTicketVault)
			router.POST("/accounts/:id/codex-ticket/vault/revoke", h.RevokeCodexTicketVault)
			path := "/accounts/" + tc.id + "/codex-ticket/vault"
			if tc.method == http.MethodPost {
				path += "/revoke"
			}
			request := httptest.NewRequest(tc.method, path, strings.NewReader(tc.body))
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, tc.status, recorder.Code, recorder.Body.String())
			require.Contains(t, recorder.Body.String(), tc.message)
		})
	}
}

func TestCodexTicketVaultRevokeAuditOmitsTicketFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &AccountHandler{codexTicketRetry: &service.OpenAIGatewayService{}}
	var extra any
	router := gin.New()
	router.POST("/accounts/:id/codex-ticket/vault/revoke", func(c *gin.Context) {
		c.Next()
		extra, _ = c.Get("audit_extra")
	}, h.RevokeCodexTicketVault)
	fingerprint := strings.Repeat("a", 24)
	request := httptest.NewRequest(http.MethodPost, "/accounts/1/codex-ticket/vault/revoke", strings.NewReader(`{"model":"gpt-x","fingerprint":"`+fingerprint+`"}`))
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Equal(t, map[string]any{"matched_count": 0, "result": "error"}, extra)
}
