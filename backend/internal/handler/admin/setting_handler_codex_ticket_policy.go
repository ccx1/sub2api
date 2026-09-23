package admin

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetCodexTicketSettings(c *gin.Context) {
	settings, err := h.settingService.GetCodexTicketSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

func (h *SettingHandler) UpdateCodexTicketSettings(c *gin.Context) {
	var settings config.OpenAICodexTicketConfig
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&settings); err != nil {
		response.BadRequest(c, "打票配置格式无效")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		response.BadRequest(c, "打票配置只能包含一个 JSON 对象")
		return
	}
	updated, err := h.settingService.UpdateCodexTicketSettings(c.Request.Context(), settings)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	middleware.SetAuditAction(c, "codex.ticket.settings.update")
	middleware.SetAuditExtra(c, map[string]any{"enabled": updated.Enabled, "credential_mode": updated.CredentialMode, "cookie_ttl_seconds": updated.CookieTTLSeconds, "cookie_refresh_before_seconds": updated.CookieRefreshBeforeSeconds, "verify_business": config.CodexTicketBusinessVerificationEnabled(updated), "business_verification_rounds": updated.BusinessVerificationRounds, "session_mode": updated.SessionMode, "refresh_strategy": updated.RefreshStrategy, "length_mode": updated.LengthMode, "tier_rule_count": len(updated.TierRules), "models": updated.Models})
	response.Success(c, updated)
}
