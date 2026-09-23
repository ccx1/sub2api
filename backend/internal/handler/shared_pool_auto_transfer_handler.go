package handler

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func ProvideSharedPoolHandler(pool *service.SharedPoolService, earnings service.SharedPoolEarningsRepository,
	keys *service.APIKeyService, billing *service.BillingCacheService, tests *service.AccountTestService,
	oauth *SharedPoolOAuthHandler, usage *service.AccountUsageService, quota *service.OpenAIQuotaService,
	recoverer *service.RateLimitService, cfg *config.Config, settings *service.SettingService,
	autoTransfer *service.SharedPoolAutoTransferService) *SharedPoolHandler {
	h := NewSharedPoolHandler(pool, earnings, keys, billing, tests, oauth, usage, quota, recoverer, cfg, settings)
	h.autoTransfer = autoTransfer
	return h
}

func (h *SharedPoolHandler) AutoTransferSettings(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok || !h.autoTransferAvailable(c) {
		return
	}
	data, err := h.autoTransfer.Get(c.Request.Context(), userID)
	sharedReply(c, data, err)
}

func (h *SharedPoolHandler) SaveAutoTransferSettings(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok || !h.autoTransferAvailable(c) {
		return
	}
	var input struct {
		Enabled   *bool   `json:"enabled"`
		Threshold float64 `json:"threshold"`
		DailyTime string  `json:"daily_time"`
	}
	if !sharedBind(c, &input) {
		return
	}
	if input.Enabled == nil {
		response.BadRequest(c, "请明确指定自动转余额开关")
		return
	}
	data, err := h.autoTransfer.Save(c.Request.Context(), userID, service.SharedPoolAutoTransferUpdate{
		Enabled: *input.Enabled, Threshold: input.Threshold, DailyTime: input.DailyTime,
	})
	sharedReply(c, data, err)
}

func (h *SharedPoolHandler) autoTransferAvailable(c *gin.Context) bool {
	if h.autoTransfer == nil {
		response.Error(c, http.StatusServiceUnavailable, "自动转余额暂不可用")
		return false
	}
	return true
}
