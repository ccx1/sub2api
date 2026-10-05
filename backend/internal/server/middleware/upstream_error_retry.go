package middleware

import (
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// UpstreamErrorRetry binds one retry budget to the inbound client request.
func UpstreamErrorRetry(settings *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c != nil && c.Request != nil {
			c.Request = c.Request.WithContext(service.WithUpstreamErrorRetry(c.Request.Context(), settings))
		}
		c.Next()
	}
}
