package admin

import (
	"strconv"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type setCodexTicketEnabledRequest struct {
	Enabled bool `json:"enabled"`
}

// SetCodexTicketEnabled updates only the account-level Codex 292 ticket switch.
// PUT /api/v1/admin/accounts/:id/codex-ticket
func (h *AccountHandler) SetCodexTicketEnabled(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	var req setCodexTicketEnabledRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}

	ctx := c.Request.Context()
	account, err := h.adminService.GetAccount(ctx, accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if account == nil || !account.IsOpenAIOAuthLike() || account.IsShadow() {
		response.ErrorFrom(c, infraerrors.BadRequest(
			"CODEX_TICKET_UNSUPPORTED_ACCOUNT",
			"Codex ticket switch is only supported for non-shadow OpenAI OAuth accounts",
		))
		return
	}

	if err := h.adminService.UpdateAccountExtra(ctx, accountID, map[string]any{
		service.OpenAICodexTicketEnabledExtraKey: req.Enabled,
	}); err != nil {
		response.ErrorFrom(c, err)
		return
	}

	updated, err := h.adminService.GetAccount(ctx, accountID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, h.buildAccountResponseWithRuntime(ctx, updated))
}
