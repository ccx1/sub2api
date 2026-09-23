package admin

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type retryCodexTicketRequest struct {
	Model string `json:"model"`
}

func (h *AccountHandler) GetCodexTicketHistory(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	page, pageErr := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, sizeErr := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageErr != nil || sizeErr != nil || page < 1 || size < 1 || size > 100 {
		response.BadRequest(c, "Invalid pagination")
		return
	}
	filter, err := service.ParseCodexTicketHistoryFilter(c.Request.URL.Query())
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if account == nil || !account.IsOpenAIOAuthLike() || account.IsShadow() {
		response.BadRequest(c, "Codex ticket history is only supported for non-shadow OpenAI OAuth accounts")
		return
	}
	history, err := service.GetCodexTicketHistory(account, page, size, filter)
	if err != nil {
		response.InternalError(c, "Unable to read Codex ticket history")
		return
	}
	response.Success(c, history)
}

// RetryCodexTicket starts one forced probe for every configured Codex model.
// The gateway still serializes concurrent work and preserves proxy health rules.
func (h *AccountHandler) RetryCodexTicket(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.codexTicketRetry == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex ticket retry is unavailable")
		return
	}
	var input retryCodexTicketRequest
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&input); err != nil && !errors.Is(err, io.EOF) {
			response.BadRequest(c, "Invalid request: "+err.Error())
			return
		}
	}
	result, err := h.codexTicketRetry.RetryOpenAICodexTicket(c.Request.Context(), id, input.Model)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrCodexTicketRetryModel):
			response.BadRequest(c, "Model is not configured for Codex ticket harvesting")
		case errors.Is(err, service.ErrCodexTicketRetryUnavailable):
			response.BadRequest(c, "Codex ticket retry is unavailable for this account")
		default:
			response.ErrorFrom(c, err)
		}
		return
	}
	response.Accepted(c, result)
}
