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

// 票库路由位于管理员鉴权之内；只返回元数据，因此不经过 step-up。
func TestCodexTicketVaultRoutesKeepAdminBoundaryWithoutStepUp(t *testing.T) {
	router := gin.New()
	authorized := false
	group := router.Group("/api/v1/admin", func(c *gin.Context) {
		if !authorized {
			c.AbortWithStatus(http.StatusUnauthorized)
		}
	})
	stepUpCalls := 0
	registerAccountRoutes(group, &handler.Handlers{Admin: &handler.AdminHandlers{
		Account: &admin.AccountHandler{},
	}}, func(c *gin.Context) { stepUpCalls++; c.Next() })
	for _, request := range []struct{ method, suffix string }{
		{http.MethodGet, "codex-ticket/vault"},
		{http.MethodPost, "codex-ticket/vault/revoke"},
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
			require.Equal(t, http.StatusBadRequest, res.Code, res.Body.String())
			require.Contains(t, res.Body.String(), "Invalid account ID")
		})
	}
	require.Zero(t, stepUpCalls)
}
