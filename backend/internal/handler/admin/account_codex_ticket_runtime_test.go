package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketRuntimeValidatesAccountBeforeReadingState(t *testing.T) {
	for _, id := range []string{"bad", "0", "-1"} {
		t.Run(id, func(t *testing.T) {
			stub := &ticketHistoryAdminStub{account: ticketHistoryAccount()}
			handler := &AccountHandler{adminService: stub}
			router := gin.New()
			router.GET("/accounts/:id/codex-ticket/runtime-status", handler.GetCodexTicketRuntimeStatus)
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/accounts/"+id+"/codex-ticket/runtime-status", nil))
			require.Equal(t, http.StatusBadRequest, res.Code)
			require.Empty(t, stub.calls)
		})
	}
	stub := &ticketHistoryAdminStub{account: ticketHistoryAccount()}
	stub.account.Type = "apikey"
	handler := &AccountHandler{adminService: stub}
	router := gin.New()
	router.GET("/accounts/:id/codex-ticket/runtime-status", handler.GetCodexTicketRuntimeStatus)
	res := httptest.NewRecorder()
	router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/accounts/41/codex-ticket/runtime-status", nil))
	require.Equal(t, http.StatusBadRequest, res.Code)
}
