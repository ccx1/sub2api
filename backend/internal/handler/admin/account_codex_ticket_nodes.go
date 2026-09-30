package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// GetCodexTicketNodes 返回票据库存、待定 Cookie 与 WS 连接池中的节点信息（测试阶段不打码）。
func (h *AccountHandler) GetCodexTicketNodes(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.codexTicketRetry == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex node capture is unavailable")
		return
	}
	result, err := h.codexTicketRetry.GetOpenAICodexTicketNodes(c.Request.Context(), id)
	if errors.Is(err, service.ErrCodexTicketNodesUnavailable) {
		response.BadRequest(c, "Node capture requires a non-shadow OpenAI OAuth account")
		return
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// ProbeCodexTicketNodes 发起一次性节点探测；结果只随响应返回，不写票池与历史。
func (h *AccountHandler) ProbeCodexTicketNodes(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}
	if h.codexTicketRetry == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex node capture is unavailable")
		return
	}
	var input service.CodexTicketNodeProbeInput
	if c.Request.Body == nil {
		response.BadRequest(c, "Invalid node probe request")
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		response.BadRequest(c, "Invalid node probe request")
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		response.BadRequest(c, "Invalid node probe request")
		return
	}
	middleware.SetAuditAction(c, "codex.ticket.node_probe")
	middleware.SetAuditExtra(c, map[string]any{"account_id": id, "model": input.Model, "source": input.Source, "slot": input.Slot, "count": input.Count})
	result, err := h.codexTicketRetry.ProbeOpenAICodexTicketNodes(c.Request.Context(), id, input)
	var probeErr *service.CodexTicketNodeProbeError
	switch {
	case errors.Is(err, service.ErrCodexTicketNodesUnavailable):
		response.BadRequest(c, "Node capture requires a non-shadow OpenAI OAuth account")
	case errors.Is(err, service.ErrCodexTicketNodeProbeBusy):
		response.Error(c, http.StatusConflict, "A node probe is already running for this account")
	case errors.As(err, &probeErr):
		response.BadRequest(c, "Node probe unavailable: "+probeErr.Reason)
	case err != nil:
		response.ErrorFrom(c, err)
	default:
		response.Success(c, result)
	}
}
