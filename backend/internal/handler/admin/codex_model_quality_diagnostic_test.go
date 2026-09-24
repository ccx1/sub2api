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

func TestDiagnoseCodexModelQualityRequiresGatewayService(t *testing.T) {
	router := gin.New()
	h := &AccountHandler{adminService: &ticketHistoryAdminStub{account: ticketHistoryAccount()}}
	router.POST("/accounts/:id/codex-ticket/diagnostic", h.DiagnoseCodexModelQuality)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/accounts/41/codex-ticket/diagnostic", strings.NewReader(`{"models":["gpt-6-astra"]}`))
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}

func TestDiagnoseCodexModelQualityRejectsInvalidBodyBeforeScheduling(t *testing.T) {
	for _, body := range []string{
		`{"model":""}`,
		`{"models":[" "]}`,
		`{"models":["gpt-6-astra"],"unknown":true}`,
		`{"models":["gpt-6-astra"]} {}`,
	} {
		router := gin.New()
		h := &AccountHandler{
			adminService:     &ticketHistoryAdminStub{account: ticketHistoryAccount()},
			codexTicketRetry: &service.OpenAIGatewayService{},
		}
		router.POST("/accounts/:id/codex-ticket/diagnostic", h.DiagnoseCodexModelQuality)
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/accounts/41/codex-ticket/diagnostic", strings.NewReader(body))
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusBadRequest, recorder.Code, body)
	}
}
