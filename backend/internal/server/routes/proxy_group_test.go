package routes

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestProxyGroupRoutesRegistered(t *testing.T) {
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{Proxy: admin.NewProxyHandler(nil)}}
	registerProxyRoutes(router.Group("/api/v1/admin"), h, func(c *gin.Context) { c.Next() })
	routes := make(map[string]bool)
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{"GET /api/v1/admin/proxy-groups", "POST /api/v1/admin/proxy-groups", "PUT /api/v1/admin/proxy-groups/:id", "DELETE /api/v1/admin/proxy-groups/:id", "POST /api/v1/admin/proxies/batch-group"} {
		require.True(t, routes[route], route)
	}
}
