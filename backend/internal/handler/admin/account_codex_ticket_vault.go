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

const codexTicketVaultAccountRequired = "Ticket vault requires a non-shadow OpenAI OAuth account"

// GetCodexTicketVault 返回账号票库的元数据（状态、时间、长度、Cookie 名、指纹），
// 不含 STATE、Cookie 值、会话或账号凭据。
func (h *AccountHandler) GetCodexTicketVault(c *gin.Context) {
	id, ok := codexTicketVaultAccountID(c)
	if !ok {
		return
	}
	if h.codexTicketRetry == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex ticket vault is unavailable")
		return
	}
	result, err := h.codexTicketRetry.GetOpenAICodexTicketVault(c.Request.Context(), id)
	if errors.Is(err, service.ErrCodexTicketNodesUnavailable) {
		response.BadRequest(c, codexTicketVaultAccountRequired)
		return
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

// RevokeCodexTicketVault 作废票库中的指定票或某模型的全部票。
func (h *AccountHandler) RevokeCodexTicketVault(c *gin.Context) {
	id, ok := codexTicketVaultAccountID(c)
	if !ok {
		return
	}
	if h.codexTicketRetry == nil {
		response.Error(c, http.StatusServiceUnavailable, "Codex ticket vault is unavailable")
		return
	}
	var input service.CodexTicketVaultRevokeInput
	if c.Request.Body == nil {
		response.BadRequest(c, "Invalid ticket vault request")
		return
	}
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		response.BadRequest(c, "Invalid ticket vault request")
		return
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		response.BadRequest(c, "Invalid ticket vault request")
		return
	}
	middleware.SetAuditAction(c, "codex.ticket.vault_revoke")
	result, err := h.codexTicketRetry.RevokeOpenAICodexTicketVault(c.Request.Context(), id, input)
	// 审计附加字段只接受白名单标量：记录作废张数与结果，账号 ID 已在路由参数中。
	outcome := "revoked"
	switch {
	case err != nil:
		outcome = "error"
	case result.Remaining > 0:
		outcome = "partial"
	case result.Revoked == 0:
		outcome = "noop"
	}
	middleware.SetAuditExtra(c, map[string]any{"matched_count": result.Revoked, "result": outcome})
	switch {
	case errors.Is(err, service.ErrCodexTicketNodesUnavailable):
		response.BadRequest(c, codexTicketVaultAccountRequired)
	case errors.Is(err, service.ErrCodexTicketVaultInvalidInput):
		response.BadRequest(c, err.Error())
	case errors.Is(err, service.ErrCodexTicketVaultSlotNotFound):
		response.NotFound(c, "Ticket not found in vault")
	case err != nil:
		response.ErrorFrom(c, err)
	default:
		response.Success(c, result)
	}
}

func codexTicketVaultAccountID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return 0, false
	}
	return id, true
}
