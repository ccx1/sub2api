//go:build unit

package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSystemHandlerDualVersionResponse(t *testing.T) {
	svc := &systemHandlerUpdateServiceStub{updateInfo: &service.UpdateInfo{
		CurrentVersion: "0.2.8.17", LatestVersion: "0.2.8", BuildType: "release",
		Official: &service.VersionSourceInfo{Repository: "Wei-Shaw/sub2api",
			CurrentVersion: "0.2.8", LatestVersion: "0.2.9", HasUpdate: true},
		Ranxi: &service.VersionSourceInfo{Repository: "ranxi2001/sub2api",
			CurrentVersion: "2.8.14", LatestVersion: "2.8.15", HasUpdate: true},
	}}
	handler := NewSystemHandler(svc, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/check-updates", handler.CheckUpdates)
	router.GET("/version", handler.GetVersion)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/check-updates?force=true", nil))
	require.Equal(t, http.StatusOK, response.Code)
	var data struct{ Data service.UpdateInfo }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &data))
	require.Equal(t, "0.2.8.17", data.Data.CurrentVersion)
	require.False(t, data.Data.HasUpdate)
	require.Equal(t, "Wei-Shaw/sub2api", data.Data.Official.Repository)
	require.True(t, data.Data.Official.HasUpdate)
	require.True(t, data.Data.Ranxi.HasUpdate)
	require.Equal(t, "2.8.14", data.Data.Ranxi.CurrentVersion)
	require.Equal(t, []bool{true}, svc.checkForces)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/version", nil))
	require.Equal(t, http.StatusOK, response.Code)
	require.Contains(t, response.Body.String(), "0.2.8.17")
	require.NotContains(t, response.Body.String(), "2.8.14")
}
