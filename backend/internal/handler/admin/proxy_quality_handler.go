package admin

import (
	"errors"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ProxyQualityHandler 对应代理池管理下的“质量管理”页。
type ProxyQualityHandler struct {
	service *service.ProxyQualityGuardService
}

func NewProxyQualityHandler(svc *service.ProxyQualityGuardService) *ProxyQualityHandler {
	return &ProxyQualityHandler{service: svc}
}

func (h *ProxyQualityHandler) requireService(c *gin.Context) bool {
	if h == nil || h.service == nil {
		response.ErrorFrom(c, errors.New("proxy quality guard service unavailable"))
		return false
	}
	return true
}

func (h *ProxyQualityHandler) Overview(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	out, err := h.service.Overview(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *ProxyQualityHandler) GetSettings(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	response.Success(c, h.service.GetSettings(c.Request.Context()))
}

func (h *ProxyQualityHandler) UpdateSettings(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	var req service.ProxyQualityGuardSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	out, err := h.service.UpdateSettings(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *ProxyQualityHandler) Events(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	events, err := h.service.Events(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if events == nil {
		events = []service.ProxyQualityGuardEvent{}
	}
	response.Success(c, gin.H{"items": events, "count": len(events)})
}

func (h *ProxyQualityHandler) RunNow(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	out, err := h.service.RunNow(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, out)
}

func (h *ProxyQualityHandler) Reset(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid proxy ID")
		return
	}
	if err := h.service.Reset(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "Proxy quality state reset successfully"})
}
