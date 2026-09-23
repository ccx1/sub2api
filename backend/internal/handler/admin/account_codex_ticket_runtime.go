package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) GetCodexTicketRuntimeStatus(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if account == nil || !account.IsOpenAIOAuthLike() || account.IsShadow() {
		response.BadRequest(c, "Codex ticket runtime is only supported for non-shadow OpenAI OAuth accounts")
		return
	}
	status, err := h.codexTicketRetry.GetOpenAICodexTicketRuntimeStatus(c.Request.Context(), id)
	if err != nil {
		response.InternalError(c, "Unable to read Codex ticket runtime")
		return
	}
	response.Success(c, status)
}
