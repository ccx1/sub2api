package handler

import (
	"context"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type sharedPoolFeatureSettings interface {
	IsSharedPoolEnabled(context.Context) bool
}

// RequireUserEntryEnabled 在管理员关闭共享池入口时拒绝用户侧接口；已分配账号的调度和管理员接口不受影响。
func (h *SharedPoolHandler) RequireUserEntryEnabled(c *gin.Context) {
	if h.feature != nil && !h.feature.IsSharedPoolEnabled(c.Request.Context()) {
		response.ErrorWithDetails(c, http.StatusForbidden, "共享账号池已关闭", "SHARED_POOL_DISABLED", nil)
		c.Abort()
		return
	}
	c.Next()
}
