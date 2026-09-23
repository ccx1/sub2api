package handler

import (
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// 以下入口仅注册在管理员认证与审计中间件之后。
func (h *SharedPoolHandler) AdminAccounts(c *gin.Context) {
	search := strings.TrimSpace(c.Query("search"))
	if len([]rune(search)) > 100 {
		response.BadRequest(c, "搜索内容过长")
		return
	}
	c.Request = c.Request.WithContext(service.WithSharedPoolSearch(c.Request.Context(), search))
	h.listAccounts(c, 0)
}
func (h *SharedPoolHandler) AdminSettings(c *gin.Context) {
	data, err := h.pool.Settings(c.Request.Context())
	sharedReply(c, data, err)
}
func (h *SharedPoolHandler) AdminSaveSettings(c *gin.Context) {
	current, err := h.pool.Settings(c.Request.Context())
	if err != nil {
		sharedReply(c, nil, err)
		return
	}
	// Start from the persisted settings so partial/legacy clients do not erase
	// default groups or subscription-tier multipliers they do not know about.
	input := *current
	input.DefaultGroupIDs = nil
	input.SubscriptionGroupIDs = nil
	input.SubscriptionSettlementMultipliers = nil
	if !sharedBind(c, &input) {
		return
	}
	if input.DefaultGroupIDs == nil {
		input.DefaultGroupIDs = current.DefaultGroupIDs
	}
	if input.SubscriptionGroupIDs == nil {
		input.SubscriptionGroupIDs = current.SubscriptionGroupIDs
	}
	if input.SubscriptionSettlementMultipliers == nil {
		input.SubscriptionSettlementMultipliers = current.SubscriptionSettlementMultipliers
	}
	err = h.pool.SaveSettings(c.Request.Context(), &input)
	sharedReply(c, &input, err)
}
func (h *SharedPoolHandler) AdminUserRates(c *gin.Context) {
	data, err := h.pool.UserRates(c.Request.Context())
	sharedReply(c, data, err)
}
func (h *SharedPoolHandler) AdminSaveUserRate(c *gin.Context) {
	id, ok := sharedID(c)
	if !ok {
		return
	}
	var input struct {
		PlatformRateBPS      *int     `json:"platform_rate_bps"`
		ProxyRateBPS         *int     `json:"proxy_rate_bps"`
		SettlementMultiplier *float64 `json:"settlement_multiplier"`
	}
	if !sharedBind(c, &input) {
		return
	}
	rate := service.SharedPoolUserRate{UserID: id, PlatformRateBPS: input.PlatformRateBPS, ProxyRateBPS: input.ProxyRateBPS, SettlementMultiplier: input.SettlementMultiplier}
	sharedReply(c, rate, h.pool.SaveUserRate(c.Request.Context(), rate))
}
func (h *SharedPoolHandler) AdminAssign(c *gin.Context) {
	id, ok := sharedID(c)
	if !ok {
		return
	}
	var input struct {
		GroupIDs         *[]int64 `json:"group_ids"`
		AdminDisabled    *bool    `json:"admin_disabled"`
		Enabled          *bool    `json:"enabled"`
		SubscriptionTier *string  `json:"subscription_tier"`
		Priority         *int     `json:"priority"`
	}
	if !sharedBind(c, &input) {
		return
	}
	if input.GroupIDs == nil && input.AdminDisabled == nil && input.Enabled == nil && input.SubscriptionTier == nil && input.Priority == nil {
		response.BadRequest(c, "请选择分组、调度状态或订阅档位")
		return
	}
	state := service.SharedPoolAccountState{GroupIDs: input.GroupIDs, AdminDisabled: input.AdminDisabled, Enabled: input.Enabled, SubscriptionTier: input.SubscriptionTier, Priority: input.Priority}
	if err := h.pool.AdminSetAccountState(c.Request.Context(), id, state); err != nil {
		sharedReply(c, nil, err)
		return
	}
	data, err := h.pool.Get(c.Request.Context(), 0, id)
	sharedReply(c, data, err)
}
func (h *SharedPoolHandler) AdminEarnings(c *gin.Context) {
	ownerID, _ := strconv.ParseInt(c.Query("owner_user_id"), 10, 64)
	if ownerID < 0 {
		response.BadRequest(c, "用户ID无效")
		return
	}
	h.listEarnings(c, ownerID)
}

func (h *SharedPoolHandler) AdminUserEarnings(c *gin.Context) {
	data, err := h.earnings.UserEarnings(c.Request.Context())
	sharedReply(c, data, err)
}
