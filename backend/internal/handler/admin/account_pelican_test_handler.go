package admin

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type pelicanTestRequest struct {
	ModelID         string `json:"model_id" binding:"required,max=100"`
	Prompt          string `json:"prompt" binding:"required,max=33000"`
	ReasoningEffort string `json:"reasoning_effort" binding:"omitempty,oneof=minimal low medium high xhigh"`
}

// PelicanTest 通过独立入口启用生成提示词，普通连通性测试保留原有行为。
// POST /api/v1/admin/accounts/:id/pelican-test
func (h *AccountHandler) PelicanTest(c *gin.Context) {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return
	}

	var req pelicanTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if strings.TrimSpace(req.ModelID) == "" || strings.TrimSpace(req.Prompt) == "" {
		response.BadRequest(c, "Model and prompt are required")
		return
	}
	if err := h.accountTestService.TestPelicanAccountConnection(c, accountID, strings.TrimSpace(req.ModelID), req.Prompt, req.ReasoningEffort); err != nil {
		// 服务已通过 SSE 返回错误，避免追加 JSON 破坏流协议。
		return
	}
	if h.rateLimitService != nil {
		if _, err := h.rateLimitService.RecoverAccountAfterSuccessfulTest(c.Request.Context(), accountID); err != nil {
			_ = c.Error(err)
		}
	}
}
