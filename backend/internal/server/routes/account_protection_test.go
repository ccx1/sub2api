//go:build unit

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountProtectionManagementRoutesRequireAdmin(t *testing.T) {
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{
		AntiDegrade: admin.NewAntiDegradeHandler(service.NewAntiDegradeService(nil)),
	}}
	registerAccountRoutes(router.Group("/api/v1/admin"), h, func(c *gin.Context) { c.Next() })
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/accounts/protection/settings"},
		{http.MethodPut, "/api/v1/admin/accounts/protection/settings"},
		{http.MethodGet, "/api/v1/admin/accounts/anti-degrade/strategies"},
		{http.MethodGet, "/api/v1/admin/accounts/1/anti-degrade"},
		{http.MethodPost, "/api/v1/admin/accounts/1/anti-degrade/apply"},
	} {
		t.Run(request.method+request.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(request.method, request.path, nil))
			require.Equal(t, http.StatusForbidden, w.Code)
		})
	}
}
