package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *AccountHandler) PreviewCodexTicketRequest(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.codexTicketRetry == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex ticket preview is unavailable")
		return
	}
	var input service.CodexTicketRequestPreviewInput
	if c.Request.Body == nil {
		response.BadRequest(c, "Invalid preview request")
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		response.BadRequest(c, "Invalid preview request")
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		response.BadRequest(c, "Invalid preview request")
		return
	}
	result, err := h.codexTicketRetry.PreviewOpenAICodexTicketRequest(c.Request.Context(), id, input)
	if errors.Is(err, service.ErrCodexTicketPreviewUnavailable) {
		response.BadRequest(c, "Preview requires a non-shadow OpenAI OAuth account")
		return
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
