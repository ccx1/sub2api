package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPelicanTestRouteReachesHandlerAndKeepsAdminBoundary(t *testing.T) {
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

	path := "/api/v1/admin/accounts/invalid/pelican-test"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
	require.Equal(t, http.StatusUnauthorized, response.Code)

	// 校验在 handler 内执行，400 能同时发现路由遗漏造成的 404。
	authorized = true
	for _, request := range []struct{ name, id, body string }{
		{"invalid ID", "invalid", `{}`},
		{"zero ID", "0", `{}`},
		{"negative ID", "-1", `{}`},
		{"malformed JSON", "42", `{`},
		{"empty body", "42", ""},
		{"blank model", "42", `{"model_id":"  ","prompt":"draw a pelican"}`},
		{"blank prompt", "42", `{"model_id":"gpt-6-astra","prompt":"  "}`},
	} {
		t.Run(request.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			path := "/api/v1/admin/accounts/" + request.id + "/pelican-test"
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(request.body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(response, req)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
		})
	}
}
