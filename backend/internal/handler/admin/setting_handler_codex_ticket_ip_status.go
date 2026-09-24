package admin

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetCodexTicketIPStatus lists egress IPs blocked by Codex ticket protection.
// GET /api/v1/admin/settings/codex-tickets/ip-status
func (h *SettingHandler) GetCodexTicketIPStatus(c *gin.Context) {
	if h == nil || h.codexIPStatusReader == nil {
		response.InternalError(c, "Codex IP status reader unavailable")
		return
	}
	status := service.CodexIPStatusKind(strings.ToLower(strings.TrimSpace(c.Query("status"))))
	if status != "" && status != service.CodexIPStatusCooling && status != service.CodexIPStatusDisabled {
		response.BadRequest(c, "status must be cooling or disabled")
		return
	}
	page, pageSize := response.ParsePagination(c)
	items, total, err := h.codexIPStatusReader.ListCodexIPStatus(c.Request.Context(), status, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}
