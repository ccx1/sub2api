package admin

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetCodexModelQualityPolicy(c *gin.Context) {
	p, err := h.settingService.GetCodexModelQualityPolicy(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, p)
}

func (h *SettingHandler) UpdateCodexModelQualityPolicy(c *gin.Context) {
	p := service.DefaultCodexModelQualityPolicy()
	if !decodeCodexModelQuality(c, &p) {
		return
	}
	updated, err := h.settingService.UpdateCodexModelQualityPolicy(c.Request.Context(), p)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	middleware.SetAuditAction(c, "codex.model_quality.settings.update")
	middleware.SetAuditExtra(c, map[string]any{"enabled": p.Enabled, "timeout_seconds": p.TimeoutSeconds, "max_ttl_percent": p.MaxTTLPercent})
	response.Success(c, updated)
}

func decodeCodexModelQuality(c *gin.Context, value any) bool {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil || !bytes.HasPrefix(bytes.TrimSpace(body), []byte("{")) {
		response.BadRequest(c, "Expected one JSON object")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		response.BadRequest(c, "Invalid model quality request")
		return false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		response.BadRequest(c, "Expected one JSON object")
		return false
	}
	return true
}

func (h *AccountHandler) codexModelQualityAccount(c *gin.Context) *service.Account {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "Invalid account ID")
		return nil
	}
	if h.adminService == nil {
		response.Error(c, http.StatusServiceUnavailable, "Account service unavailable")
		return nil
	}
	account, err := h.adminService.GetAccount(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return nil
	}
	if account == nil || !account.IsOpenAIOAuthLike() || account.IsShadow() {
		response.BadRequest(c, "Model quality requires a non-shadow OpenAI OAuth account")
		return nil
	}
	return account
}

func (h *AccountHandler) GetCodexModelQuality(c *gin.Context) {
	account := h.codexModelQualityAccount(c)
	if account == nil {
		return
	}
	if h.codexTicketRetry == nil {
		response.Error(c, http.StatusServiceUnavailable, "Model quality service unavailable")
		return
	}
	var items []service.CodexModelQualityStatus
	var err error
	if strings.EqualFold(strings.TrimSpace(c.Query("schedule")), "false") {
		items, err = h.codexTicketRetry.GetCodexModelQualityStatusSnapshot(c.Request.Context(), account)
	} else {
		items, err = h.codexTicketRetry.GetCodexModelQualityStatuses(c.Request.Context(), account)
	}
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"items": items})
}

func (h *AccountHandler) RetryCodexModelQuality(c *gin.Context) {
	account := h.codexModelQualityAccount(c)
	if account == nil {
		return
	}
	var body struct {
		Model string `json:"model"`
	}
	if !decodeCodexModelQuality(c, &body) {
		return
	}
	if strings.TrimSpace(body.Model) == "" || len(body.Model) > 160 {
		response.BadRequest(c, "Invalid model")
		return
	}
	if h.codexTicketRetry == nil {
		response.Error(c, http.StatusServiceUnavailable, "Model quality service unavailable")
		return
	}
	result, err := h.codexTicketRetry.RetryCodexModelQuality(c.Request.Context(), account, body.Model)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	middleware.SetAuditAction(c, "codex.model_quality.retry")
	middleware.SetAuditExtra(c, map[string]any{"account_id": account.ID, "model": body.Model, "scheduled": result.Scheduled})
	response.Success(c, result)
}
