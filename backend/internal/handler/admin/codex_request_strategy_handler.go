package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetCodexRequestStrategyPolicy(c *gin.Context) {
	policy, err := h.settingService.GetCodexRequestStrategyPolicy(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, policy)
}

func (h *SettingHandler) UpdateCodexRequestStrategyPolicy(c *gin.Context) {
	policy := service.DefaultCodexRequestStrategyPolicy()
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil || !bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
		response.BadRequest(c, "请求策略配置格式无效")
		return
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		response.BadRequest(c, "请求策略配置格式无效")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		response.BadRequest(c, "请求策略配置只能包含一个 JSON 对象")
		return
	}
	updated, err := h.settingService.UpdateCodexRequestStrategyPolicy(c.Request.Context(), policy)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	middleware.SetAuditAction(c, "codex.request_strategy.settings.update")
	middleware.SetAuditExtra(c, map[string]any{
		"enabled":                        updated.Enabled,
		"strategy":                       updated.Strategy,
		"scope":                          updated.Scope,
		"failure_mode":                   updated.FailureMode,
		"cookie_mode":                    updated.CookieMode,
		"route_affinity_mode":            updated.RouteAffinityMode,
		"route_prewarm_connections":      updated.RoutePrewarmConnections,
		"route_failure_cooldown_seconds": updated.RouteFailureCooldownSeconds,
		"region_mode":                    updated.RegionMode,
		"time_context_mode":              updated.TimeContextMode,
		"compliance_mode":                updated.ComplianceMode,
	})
	response.Success(c, updated)
}
