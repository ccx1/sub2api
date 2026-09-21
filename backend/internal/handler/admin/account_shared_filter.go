package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func applySharedAccountQuery(c *gin.Context) bool {
	value := c.Query("shared")
	switch value {
	case "", "all", "shared", "platform":
		c.Request = c.Request.WithContext(service.WithSharedAccountFilter(c.Request.Context(), value))
		return true
	default:
		response.BadRequest(c, "无效的共享账号筛选条件")
		return false
	}
}
