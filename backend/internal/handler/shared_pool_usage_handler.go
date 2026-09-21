package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type sharedAccountUsageService interface {
	GetUsageForAccount(context.Context, *service.Account, ...bool) (*service.UsageInfo, error)
	GetPassiveUsage(context.Context, int64) (*service.UsageInfo, error)
}

// 只暴露用量展示字段，避免把上游错误、验证链接或后续新增的管理信息传给用户。
type sharedAccountUsageView struct {
	Source                   string                                    `json:"source,omitempty"`
	UpdatedAt                *time.Time                                `json:"updated_at,omitempty"`
	FiveHour                 *service.UsageProgress                    `json:"five_hour"`
	SevenDay                 *service.UsageProgress                    `json:"seven_day,omitempty"`
	SevenDaySonnet           *service.UsageProgress                    `json:"seven_day_sonnet,omitempty"`
	SevenDayFable            *service.UsageProgress                    `json:"seven_day_fable,omitempty"`
	GeminiSharedDaily        *service.UsageProgress                    `json:"gemini_shared_daily,omitempty"`
	GeminiProDaily           *service.UsageProgress                    `json:"gemini_pro_daily,omitempty"`
	GeminiFlashDaily         *service.UsageProgress                    `json:"gemini_flash_daily,omitempty"`
	GeminiSharedMinute       *service.UsageProgress                    `json:"gemini_shared_minute,omitempty"`
	GeminiProMinute          *service.UsageProgress                    `json:"gemini_pro_minute,omitempty"`
	GeminiFlashMinute        *service.UsageProgress                    `json:"gemini_flash_minute,omitempty"`
	AntigravityQuota         map[string]*service.AntigravityModelQuota `json:"antigravity_quota,omitempty"`
	IsForbidden              bool                                      `json:"is_forbidden,omitempty"`
	NeedsVerify              bool                                      `json:"needs_verify,omitempty"`
	IsBanned                 bool                                      `json:"is_banned,omitempty"`
	NeedsReauth              bool                                      `json:"needs_reauth,omitempty"`
	ErrorCode                string                                    `json:"error_code,omitempty"`
	Error                    string                                    `json:"error,omitempty"`
	CodexTurnTickets         []service.OpenAICodexTicketStatus         `json:"codex_turn_tickets,omitempty"`
	CodexResetCreditSnapshot *service.OpenAIRateLimitResetCredits      `json:"codex_reset_credit_snapshot,omitempty"`
}

func (h *SharedPoolHandler) GetUsage(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	id, ok := sharedID(c)
	if !ok {
		return
	}
	_, account, err := h.pool.OwnedAccount(c.Request.Context(), userID, id)
	if err != nil {
		sharedReply(c, nil, err)
		return
	}
	if account.Type != service.AccountTypeOAuth {
		response.BadRequest(c, "仅 OAuth 账号支持用量窗口")
		return
	}
	source := c.DefaultQuery("source", "active")
	forceRaw := c.Query("force")
	force := forceRaw == "true"
	if (source != "active" && source != "passive") ||
		(source == "passive" && (account.Platform != service.PlatformAnthropic || force)) ||
		(forceRaw != "" && forceRaw != "true" && forceRaw != "false") || len(c.Request.URL.Query()["force"]) > 1 {
		response.BadRequest(c, "用量查询参数无效")
		return
	}
	if h.usage == nil {
		response.Error(c, http.StatusServiceUnavailable, "用量查询暂不可用")
		return
	}
	if source == "active" {
		action := sharedAccountUsageReadAction
		if force {
			action = "usage-refresh"
		}
		release, allowed := h.guardSharedAccountAction(c, id, action)
		if !allowed {
			return
		}
		defer release()
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	var usage *service.UsageInfo
	if source == "passive" {
		usage, err = h.usage.GetPassiveUsage(ctx, account.ID)
	} else {
		usage, err = h.usage.GetUsageForAccount(ctx, account, force)
	}
	if err != nil || usage == nil {
		response.Error(c, http.StatusBadGateway, "用量查询失败，请稍后重试")
		return
	}
	view := sharedUsageView(usage, source)
	if account.IsOpenAIOAuthLike() && !account.IsShadow() {
		if _, fresh, loadErr := h.pool.OwnedAccount(ctx, userID, id); loadErr == nil && fresh != nil {
			account = fresh
		}
	}
	h.enrichSharedUsageView(ctx, &view, account)
	response.Success(c, view)
}

func sharedUsageView(usage *service.UsageInfo, source string) sharedAccountUsageView {
	view := sharedAccountUsageView{
		Source: source, UpdatedAt: usage.UpdatedAt, FiveHour: usage.FiveHour,
		SevenDay: usage.SevenDay, SevenDaySonnet: usage.SevenDaySonnet, SevenDayFable: usage.SevenDayFable,
		GeminiSharedDaily: usage.GeminiSharedDaily, GeminiProDaily: usage.GeminiProDaily, GeminiFlashDaily: usage.GeminiFlashDaily,
		GeminiSharedMinute: usage.GeminiSharedMinute, GeminiProMinute: usage.GeminiProMinute, GeminiFlashMinute: usage.GeminiFlashMinute,
		AntigravityQuota: usage.AntigravityQuota, IsForbidden: usage.IsForbidden,
		NeedsVerify: usage.NeedsVerify, IsBanned: usage.IsBanned, NeedsReauth: usage.NeedsReauth,
	}
	if usage.Source == "active" || usage.Source == "passive" {
		view.Source = usage.Source
	}
	switch usage.ErrorCode {
	case "forbidden", "unauthenticated", "rate_limited", "network_error":
		view.ErrorCode = usage.ErrorCode
	}
	if usage.Error != "" || usage.ForbiddenReason != "" || usage.ErrorCode != "" ||
		usage.IsForbidden || usage.NeedsVerify || usage.IsBanned || usage.NeedsReauth {
		view.Error = "用量暂不可用，请稍后重试或重新授权"
	}
	return view
}
