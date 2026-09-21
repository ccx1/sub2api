package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AntiDegradeHandler) GetProtectionSettings(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	settings, err := h.service.GetProtectionSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *AntiDegradeHandler) UpdateProtectionSettings(c *gin.Context) {
	if !h.requireService(c) {
		return
	}
	var req service.AccountProtectionSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid protection settings")
		return
	}
	middleware.SetAuditAction(c, "account.protection.settings.update")
	middleware.SetAuditExtra(c, map[string]any{"default_mode": req.DefaultMode})
	settings, err := h.service.UpdateProtectionSettings(c.Request.Context(), req.DefaultMode)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}
