package admin

import (
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

// DiagnoseCodexModelQuality starts an operator-triggered quality diagnosis.
// The worker and persistence path are shared with the normal model-quality
// checker, so this endpoint never handles ticket or cookie material itself.
func (h *AccountHandler) DiagnoseCodexModelQuality(c *gin.Context) {
	account := h.codexModelQualityAccount(c)
	if account == nil {
		return
	}
	if h.codexTicketRetry == nil {
		response.Error(c, http.StatusServiceUnavailable, "Model quality service unavailable")
		return
	}
	var input struct {
		Model  *string  `json:"model"`
		Models []string `json:"models"`
	}
	if c.Request.Body != nil && c.Request.ContentLength != 0 {
		if !decodeCodexModelQuality(c, &input) {
			return
		}
	}
	models := make([]string, 0, len(input.Models)+1)
	if input.Model != nil {
		models = append(models, strings.TrimSpace(*input.Model))
	}
	for _, model := range input.Models {
		models = append(models, strings.TrimSpace(model))
	}
	result, err := h.codexTicketRetry.DiagnoseCodexModelQuality(c.Request.Context(), account, models)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	middleware.SetAuditAction(c, "codex.model_quality.diagnostic")
	middleware.SetAuditExtra(c, map[string]any{"account_id": account.ID, "models": len(result.Items)})
	response.Accepted(c, result)
}
