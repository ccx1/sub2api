package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type SharedPoolHandler struct {
	autoTransfer   *service.SharedPoolAutoTransferService
	pool           *service.SharedPoolService
	earnings       service.SharedPoolEarningsRepository
	keys           *service.APIKeyService
	billing        *service.BillingCacheService
	tests          *service.AccountTestService
	usage          sharedAccountUsageService
	oauth          *SharedPoolOAuthHandler
	mu             sync.Mutex
	lastTest       map[int64]time.Time
	quota          sharedQuotaService
	recoverer      sharedAccountStateRecoverer
	ticketConfig   *config.Config
	ticketSettings sharedCodexTicketSettings
	actions        sharedAccountActions
	importSettings sharedAccountImportSettings
}

func NewSharedPoolHandler(pool *service.SharedPoolService, earnings service.SharedPoolEarningsRepository, keys *service.APIKeyService,
	billing *service.BillingCacheService, tests *service.AccountTestService, oauth *SharedPoolOAuthHandler, usage *service.AccountUsageService,
	quota *service.OpenAIQuotaService, recoverer *service.RateLimitService, cfg *config.Config, settings *service.SettingService) *SharedPoolHandler {
	h := &SharedPoolHandler{pool: pool, earnings: earnings, keys: keys, billing: billing, tests: tests, oauth: oauth, lastTest: map[int64]time.Time{}}
	if usage != nil {
		h.usage = usage
	}
	if quota != nil {
		h.quota = quota
	}
	if recoverer != nil {
		h.recoverer = recoverer
	}
	h.ticketConfig = cfg
	if settings != nil {
		h.ticketSettings = settings
		h.importSettings = settings
	}
	return h
}

func sharedUser(c *gin.Context) (int64, bool) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "请先登录")
		return 0, false
	}
	return subject.UserID, true
}

func sharedID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "ID无效")
		return 0, false
	}
	return id, true
}

func sharedBind(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		response.BadRequest(c, "请求字段或格式无效")
		return false
	}
	if decoder.Decode(new(any)) != io.EOF {
		response.BadRequest(c, "请求格式无效")
		return false
	}
	return true
}

func sharedReply(c *gin.Context, data any, err error) {
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, data)
}

func (h *SharedPoolHandler) Config(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	data, err := h.pool.UserConfig(c.Request.Context(), userID)
	if err == nil {
		var defaults *sharedAccountImportDefaults
		defaults, err = h.accountImportDefaults(c.Request.Context())
		if defaults != nil {
			data["import_defaults"] = defaults
		}
	}
	sharedReply(c, data, err)
}

func (h *SharedPoolHandler) Pools(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	groups, err := h.keys.GetAvailableGroups(c.Request.Context(), userID)
	if err != nil {
		sharedReply(c, nil, err)
		return
	}
	groups, err = h.keys.SharedAccountCatalogGroups(c.Request.Context(), groups)
	if err != nil {
		sharedReply(c, nil, err)
		return
	}
	result := make([]sharedPoolCatalogView, 0)
	cfg, now := h.sharedTicketConfig(c.Request.Context()), time.Now()
	counts := h.keys.SharedPoolCurrentConcurrency(c.Request.Context(), groups, cfg, now)
	for _, g := range groups {
		if g.SharedPoolCapacity != nil {
			view := newSharedPoolCatalogView(g, cfg, now)
			if view.AvailableAccounts <= 0 {
				continue
			}
			if current, known := counts[g.ID]; known {
				view.CurrentConcurrency = &current
			}
			result = append(result, view)
		}
	}
	response.Success(c, result)
}

func (h *SharedPoolHandler) Accounts(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	h.listAccounts(c, userID)
}

func (h *SharedPoolHandler) listAccounts(c *gin.Context, ownerID int64) {
	page, size := response.ParsePagination(c)
	if size > 100 {
		size = 100
	}
	data, err := h.pool.List(c.Request.Context(), ownerID, page, size)
	sharedReply(c, data, err)
}

func (h *SharedPoolHandler) Create(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	req := sharedAccountCreateRequest{SharedPoolAccountInput: service.SharedPoolAccountInput{ProtectionEnabled: true, Concurrency: 3, Type: service.AccountTypeOAuth}}
	if !sharedBind(c, &req) {
		return
	}
	defaults, err := h.accountImportDefaults(c.Request.Context())
	if err != nil {
		sharedReply(c, nil, err)
		return
	}
	input := req.SharedPoolAccountInput
	applySharedAccountImportDefaults(&input, defaults, sharedImportDefaults{ProtectionEnabled: req.ProtectionEnabled,
		ExcelBPSEnabled: input.ExcelBPSEnabled, ExcelBPSOptions: input.ExcelBPSOptions})
	data, err := h.pool.Create(c.Request.Context(), userID, input)
	sharedReply(c, data, err)
}

func (h *SharedPoolHandler) Update(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	id, ok := sharedID(c)
	if !ok {
		return
	}
	var input service.SharedPoolAccountInput
	if !sharedBind(c, &input) {
		return
	}
	data, err := h.pool.Update(c.Request.Context(), userID, id, input)
	sharedReply(c, data, err)
}

func (h *SharedPoolHandler) Remove(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	id, ok := sharedID(c)
	if !ok {
		return
	}
	sharedReply(c, map[string]bool{"success": true}, h.pool.Remove(c.Request.Context(), userID, id))
}

func (h *SharedPoolHandler) SetEnabled(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	id, ok := sharedID(c)
	if !ok {
		return
	}
	var input struct {
		Enabled         bool  `json:"enabled"`
		DispatchConsent *bool `json:"dispatch_consent"`
	}
	if !sharedBind(c, &input) {
		return
	}
	if err := h.pool.SetEnabled(c.Request.Context(), userID, id, input.Enabled, input.DispatchConsent); err != nil {
		sharedReply(c, nil, err)
		return
	}
	data, err := h.pool.Get(c.Request.Context(), userID, id)
	sharedReply(c, data, err)
}

func (h *SharedPoolHandler) SetProtection(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	id, ok := sharedID(c)
	if !ok {
		return
	}
	var input struct {
		Enabled bool `json:"enabled"`
		Confirm bool `json:"confirm_disable"`
	}
	if !sharedBind(c, &input) {
		return
	}
	if err := h.pool.SetProtection(c.Request.Context(), userID, id, input.Enabled, input.Confirm); err != nil {
		sharedReply(c, nil, err)
		return
	}
	data, err := h.pool.Get(c.Request.Context(), userID, id)
	sharedReply(c, data, err)
}

func (h *SharedPoolHandler) Summary(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	data, err := h.earnings.Summary(c.Request.Context(), userID)
	sharedReply(c, data, err)
}

func (h *SharedPoolHandler) Earnings(c *gin.Context) {
	userID, ok := sharedUser(c)
	if ok {
		h.listEarnings(c, userID)
	}
}

func (h *SharedPoolHandler) listEarnings(c *gin.Context, ownerID int64) {
	page, size := response.ParsePagination(c)
	if size > 100 {
		size = 100
	}
	accountID, _ := strconv.ParseInt(c.Query("account_id"), 10, 64)
	data, err := h.earnings.List(c.Request.Context(), ownerID, service.SharedPoolEarningsFilter{Page: page, PageSize: size, AccountID: accountID, Status: c.Query("status")})
	sharedReply(c, data, err)
}

func (h *SharedPoolHandler) Transfer(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	h.transferEarnings(c, userID)
}

// transferEarnings 统一处理用户和管理员发起的共享收益转入，确保余额缓存和认证缓存同步失效。
func (h *SharedPoolHandler) transferEarnings(c *gin.Context, userID int64) {
	data, err := h.earnings.Transfer(c.Request.Context(), userID)
	if err == nil {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 5*time.Second)
		defer cancel()
		if h.keys != nil {
			h.keys.InvalidateAuthCacheByUserID(ctx, userID)
		}
		if h.billing != nil {
			_ = h.billing.InvalidateUserBalance(ctx, userID)
		}
	}
	sharedReply(c, data, err)
}

func (h *SharedPoolHandler) OAuthStart(c *gin.Context)  { h.oauth.Start(c) }
func (h *SharedPoolHandler) OAuthFinish(c *gin.Context) { h.oauth.Finish(c) }
