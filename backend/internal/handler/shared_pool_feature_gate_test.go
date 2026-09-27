//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type sharedPoolFeatureStub bool

func (s sharedPoolFeatureStub) IsSharedPoolEnabled(context.Context) bool { return bool(s) }

func TestSharedPoolUserEntryGate(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name    string
		feature sharedPoolFeatureSettings
		status  int
	}{
		{"enabled", sharedPoolFeatureStub(true), http.StatusNoContent},
		{"disabled", sharedPoolFeatureStub(false), http.StatusForbidden},
		{"no settings", nil, http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := &SharedPoolHandler{feature: tc.feature}
			router := gin.New()
			router.GET("/shared-pool/accounts", h.RequireUserEntryEnabled, func(c *gin.Context) { c.Status(http.StatusNoContent) })
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/shared-pool/accounts", nil))
			require.Equal(t, tc.status, w.Code)
			if tc.status == http.StatusForbidden {
				require.Contains(t, w.Body.String(), "SHARED_POOL_DISABLED")
			}
		})
	}
}
