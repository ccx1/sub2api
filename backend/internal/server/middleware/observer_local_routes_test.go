//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestObserverLocalAdministrativeRoutesRequireAdmin(t *testing.T) {
	router, token := observerLocalAuthRouter(t)
	call := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, strings.ReplaceAll(path, ":id", "1"), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	router.GET("/api/v1/admin/accounts", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	require.Equal(t, http.StatusNoContent, call(http.MethodGet, "/api/v1/admin/accounts").Code)
	for _, route := range observerLocalAdministrativeRoutes() {
		t.Run(route.method+" "+route.path, func(t *testing.T) {
			hit := false
			router.Handle(route.method, route.path, func(c *gin.Context) {
				hit = true
				c.Status(http.StatusNoContent)
			})
			w := call(route.method, route.path)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			require.False(t, hit, "local administrative handler must not run for observers")
		})
	}
}

func observerLocalAdministrativeRoutes() []struct{ method, path string } {
	return []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/shared-pool/settings"},
		{http.MethodPut, "/api/v1/admin/shared-pool/settings"},
		{http.MethodGet, "/api/v1/admin/shared-pool/accounts"},
		{http.MethodPut, "/api/v1/admin/shared-pool/accounts/:id"},
		{http.MethodGet, "/api/v1/admin/shared-pool/user-rates"},
		{http.MethodPut, "/api/v1/admin/shared-pool/user-rates/:id"},
		{http.MethodGet, "/api/v1/admin/shared-pool/earnings"},
		{http.MethodGet, "/api/v1/admin/shared-pool/user-earnings"},
		{http.MethodPost, "/api/v1/admin/shared-pool/users/:id/transfer"},
		{http.MethodGet, "/api/v1/admin/spend-guard"},
		{http.MethodGet, "/api/v1/admin/spend-guard/settings"},
		{http.MethodPut, "/api/v1/admin/spend-guard/settings"},
		{http.MethodGet, "/api/v1/admin/spend-guard/events"},
		{http.MethodPost, "/api/v1/admin/spend-guard/keys/:id/unfreeze"},
		{http.MethodGet, "/api/v1/admin/security-policy/keywords/builtin"},
		{http.MethodGet, "/api/v1/admin/security-policy/keywords"},
		{http.MethodPost, "/api/v1/admin/security-policy/keywords"},
		{http.MethodPut, "/api/v1/admin/security-policy/keywords/:id"},
		{http.MethodDelete, "/api/v1/admin/security-policy/keywords/:id"},
		{http.MethodPost, "/api/v1/admin/security-policy/sessions/unblock"},
		{http.MethodPut, "/api/v1/admin/settings"},
		{http.MethodPost, "/api/v1/admin/proxy-groups"},
		{http.MethodPut, "/api/v1/admin/proxy-groups/:id"},
		{http.MethodPut, "/api/v1/admin/proxies/:id"},
		{http.MethodPost, "/api/v1/admin/proxies/batch-group"},
		{http.MethodGet, "/api/v1/admin/accounts/protection/settings"},
		{http.MethodPut, "/api/v1/admin/accounts/protection/settings"},
	}
}

func observerLocalAuthRouter(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{JWT: config.JWTConfig{Secret: "observer-local-test", ExpireHour: 1}}
	auth := service.NewAuthService(nil, nil, nil, nil, cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	user := &service.User{ID: 12, Role: service.RoleObserver, Status: service.StatusActive, ObserverGroupIDs: []int64{7}}
	repo := &stubUserRepo{getByID: func(context.Context, int64) (*service.User, error) {
		clone := *user
		return &clone, nil
	}}
	users := service.NewUserService(repo, nil, nil, nil)
	token, err := auth.GenerateToken(context.Background(), user)
	require.NoError(t, err)
	router := gin.New()
	router.Use(gin.HandlerFunc(NewAdminAuthMiddleware(auth, users, nil, nil)))
	return router, token
}
