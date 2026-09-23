//go:build unit

package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSharedAccountTestRoutesRequireUserIdentity(t *testing.T) {
	router := gin.New()
	registerSharedPoolRoutes(router.Group("/api/v1"), &handler.SharedPoolHandler{})
	for _, request := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/shared-pool/overview"},
		{http.MethodGet, "/api/v1/shared-pool/auto-transfer"},
		{http.MethodPut, "/api/v1/shared-pool/auto-transfer"},
		{http.MethodGet, "/api/v1/shared-pool/accounts/1/models"},
		{http.MethodGet, "/api/v1/shared-pool/accounts/1/usage"},
		{http.MethodPost, "/api/v1/shared-pool/accounts/1/test"},
		{http.MethodPost, "/api/v1/shared-pool/accounts/1/quota/refresh"},
		{http.MethodPost, "/api/v1/shared-pool/accounts/1/reset-quota"},
	} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(request.method, request.path, nil))
		require.Equal(t, http.StatusUnauthorized, w.Code)
	}
}

func TestSharedCodexTicketRouteRequiresUserIdentity(t *testing.T) {
	router := gin.New()
	registerSharedPoolRoutes(router.Group("/api/v1"), &handler.SharedPoolHandler{})
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/shared-pool/accounts/1/codex-ticket", nil))
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
