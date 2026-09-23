package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountFeatureFusionRoutesKeepAdminBoundary(t *testing.T) {
	router := gin.New()
	authorized := false
	group := router.Group("/api/v1/admin", func(c *gin.Context) {
		if !authorized {
			c.AbortWithStatus(http.StatusUnauthorized)
		}
	})
	registerAccountRoutes(group, &handler.Handlers{Admin: &handler.AdminHandlers{
		Account: &admin.AccountHandler{},
	}}, func(c *gin.Context) { c.Next() })
	for _, request := range []struct {
		method, suffix string
		status         int
	}{
		{http.MethodGet, "codex-ticket/runtime-status", http.StatusBadRequest},
		{http.MethodPost, "codex-ticket/request-preview", http.StatusBadRequest},
		{http.MethodGet, "opencode-go-usage", http.StatusServiceUnavailable},
		{http.MethodPost, "opencode-go-usage/refresh", http.StatusServiceUnavailable},
	} {
		t.Run(request.suffix, func(t *testing.T) {
			path := "/api/v1/admin/accounts/invalid/" + request.suffix
			authorized = false
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(request.method, path, nil))
			require.Equal(t, http.StatusUnauthorized, res.Code)
			authorized = true
			res = httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(request.method, path, nil))
			require.Equal(t, request.status, res.Code, res.Body.String())
			if request.status == http.StatusServiceUnavailable {
				require.Contains(t, res.Body.String(), "OPENCODE_GO_USAGE_UNAVAILABLE")
			}
		})
	}
}
