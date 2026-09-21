package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type sharedQuotaService interface {
	QueryUsage(context.Context, int64) (*service.OpenAIQuotaUsage, error)
	CacheResetCreditsSnapshot(context.Context, int64, *service.OpenAIRateLimitResetCredits) error
	CachePostResetSnapshot(context.Context, int64, *service.OpenAIQuotaUsage) error
	ResetCredit(context.Context, int64) (*service.OpenAIQuotaResetResult, error)
}

type sharedAccountStateRecoverer interface {
	RecoverAccountState(context.Context, int64, service.AccountRecoveryOptions) (*service.SuccessfulTestRecoveryResult, error)
}

type sharedQuotaRefreshResponse struct {
	sharedQuotaView
	CachePersisted bool `json:"cache_persisted"`
}

type sharedQuotaResetResponse struct {
	Code                  string           `json:"code"`
	WindowsReset          int              `json:"windows_reset"`
	Quota                 *sharedQuotaView `json:"quota,omitempty"`
	CacheRefreshed        bool             `json:"cache_refreshed"`
	AccountStateRecovered bool             `json:"account_state_recovered"`
	WarningCode           string           `json:"warning_code,omitempty"`
}

func (h *SharedPoolHandler) sharedQuotaAccount(c *gin.Context) (int64, int64, bool) {
	owner, ok := sharedUser(c)
	if !ok {
		return 0, 0, false
	}
	id, ok := sharedID(c)
	if !ok {
		return 0, 0, false
	}
	_, account, err := h.pool.OwnedAccount(c.Request.Context(), owner, id)
	if err != nil {
		sharedReply(c, nil, err)
		return 0, 0, false
	}
	if account == nil || !account.IsOpenAIOAuth() || account.IsShadow() {
		response.BadRequest(c, "仅非影子 OpenAI OAuth 账号支持额度卡操作")
		return 0, 0, false
	}
	if h.quota == nil {
		response.Error(c, http.StatusServiceUnavailable, "额度查询暂不可用")
		return 0, 0, false
	}
	if c.Request.ContentLength != 0 && !sharedBind(c, &struct{}{}) {
		return 0, 0, false
	}
	return owner, id, true
}

func (h *SharedPoolHandler) RefreshQuota(c *gin.Context) {
	_, id, ok := h.sharedQuotaAccount(c)
	if !ok {
		return
	}
	release, ok := h.guardSharedAccountAction(c, id, "quota-refresh")
	if !ok {
		return
	}
	defer release()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	usage, err := h.quota.QueryUsage(ctx, id)
	if err != nil || usage == nil {
		response.Error(c, http.StatusBadGateway, "额度查询失败，请稍后重试")
		return
	}
	service.NotifyOpenAIAutoResetCredit(id)
	view := sharedQuotaRefreshResponse{sharedQuotaView: *sharedQuotaUsageView(usage)}
	view.CachePersisted = h.quota.CacheResetCreditsSnapshot(ctx, id, usage.RateLimitResetCredits) == nil
	response.Success(c, view)
}

func (h *SharedPoolHandler) ResetQuota(c *gin.Context) {
	owner, id, ok := h.sharedQuotaAccount(c)
	if !ok {
		return
	}
	release, ok := h.guardSharedAccountAction(c, id, "quota-reset")
	if !ok {
		return
	}
	defer release()
	result, err := h.quota.ResetCredit(c.Request.Context(), id)
	if err != nil || result == nil {
		response.Error(c, http.StatusBadGateway, "额度卡操作未确认成功，请刷新额度后核实，勿重复用卡")
		return
	}
	// 上游已消耗额度卡，断开浏览器不能中断本地账号恢复。
	postCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 8*time.Second)
	defer cancel()
	post := service.RunOpenAIQuotaResetPostProcess(postCtx, id, h.quota, h.recoverer,
		func(ctx context.Context, id int64) (*service.Account, error) {
			_, account, err := h.pool.OwnedAccount(ctx, owner, id)
			return account, err
		})
	code := "success"
	if result.Code == "ok" {
		code = "ok"
	}
	response.Success(c, sharedQuotaResetResponse{Code: code, WindowsReset: max(0, result.WindowsReset), Quota: sharedQuotaUsageView(post.Quota),
		CacheRefreshed: post.CacheRefreshed, AccountStateRecovered: post.AccountStateRecovered, WarningCode: post.WarningCode})
}
