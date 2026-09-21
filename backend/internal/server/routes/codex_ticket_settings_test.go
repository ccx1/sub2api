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

func TestCodexTicketSettingsRoutesUseAdminGroup(t *testing.T) {
	router := gin.New()
	authorized := false
	group := router.Group("/api/v1/admin", func(c *gin.Context) {
		if !authorized {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	})
	registerSettingsRoutes(group, &handler.Handlers{Admin: &handler.AdminHandlers{Setting: &admin.SettingHandler{}}})
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, "/api/v1/admin/settings/codex-tickets", nil))
		require.Equal(t, http.StatusUnauthorized, w.Code)
	}
	authorized = true
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/settings/codex-tickets", nil))
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
}
