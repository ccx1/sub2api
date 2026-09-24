package admin

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetAccountImportSettings(c *gin.Context) {
	if h.settingService == nil {
		response.Error(c, http.StatusServiceUnavailable, "账号导入配置服务不可用")
		return
	}
	settings, err := h.settingService.GetAccountImportSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *SettingHandler) UpdateAccountImportSettings(c *gin.Context) {
	var req struct {
		Enabled            *bool          `json:"enabled" binding:"required"`
		ProtectionEnabled  *bool          `json:"protection_enabled" binding:"required"`
		CodexTicketEnabled *bool          `json:"codex_ticket_enabled" binding:"required"`
		ProxyMode          string         `json:"proxy_mode" binding:"required,oneof=preserve direct fixed random"`
		ProxyID            *int64         `json:"proxy_id"`
		Extra              map[string]any `json:"extra"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if h.settingService == nil {
		response.Error(c, http.StatusServiceUnavailable, "账号导入配置服务不可用")
		return
	}
	settings, err := h.settingService.UpdateAccountImportSettings(c.Request.Context(), service.AccountImportSettings{
		Enabled: *req.Enabled, ProtectionEnabled: *req.ProtectionEnabled,
		CodexTicketEnabled: *req.CodexTicketEnabled, ProxyMode: req.ProxyMode,
		ProxyID: req.ProxyID, Extra: req.Extra,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}
