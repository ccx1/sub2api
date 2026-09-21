package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type sharedAccountTestRequest struct {
	ModelID string `json:"model_id"`
	Prompt  string `json:"prompt"`
	Mode    string `json:"mode"`
}

func (h *SharedPoolHandler) ownedTestAccount(c *gin.Context) (int64, *service.Account, bool) {
	userID, ok := sharedUser(c)
	if !ok {
		return 0, nil, false
	}
	id, ok := sharedID(c)
	if !ok {
		return 0, nil, false
	}
	record, account, err := h.pool.OwnedAccount(c.Request.Context(), userID, id)
	if err != nil {
		sharedReply(c, nil, err)
		return 0, nil, false
	}
	if record.AdminDisabled {
		response.Forbidden(c, "管理员已停用该账号")
		return 0, nil, false
	}
	return userID, account, true
}

func (h *SharedPoolHandler) GetAvailableModels(c *gin.Context) {
	_, account, ok := h.ownedTestAccount(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	response.Success(c, h.tests.GetAvailableModels(ctx, account))
}

func (h *SharedPoolHandler) Test(c *gin.Context) {
	userID, account, ok := h.ownedTestAccount(c)
	if !ok {
		return
	}
	input, ok := bindSharedAccountTest(c)
	if !ok {
		return
	}
	if input.Mode == service.AccountTestModeCompact && !account.IsOpenAI() {
		response.BadRequest(c, "仅 OpenAI 账号支持压缩测试")
		return
	}
	if h.tests == nil {
		response.Error(c, http.StatusServiceUnavailable, "连接测试服务暂不可用")
		return
	}
	if !h.allowAccountTest(userID) {
		response.Error(c, http.StatusTooManyRequests, "每分钟最多测试一次")
		return
	}
	// 图片测试复用管理员的上游流程，不能被原来的 45 秒文本探测期限截断。
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Minute)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	opts := service.AccountTestOptions{RedactErrors: true}
	if err := h.tests.TestAccountConnection(c, account.ID, input.ModelID, input.Prompt, input.Mode, opts); err != nil && !c.Writer.Written() {
		response.BadRequest(c, "连接测试失败，请稍后重试")
	}
}

func bindSharedAccountTest(c *gin.Context) (sharedAccountTestRequest, bool) {
	var input sharedAccountTestRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&input)
	// 兼容旧版无请求体的测试调用，同时拒绝非法 JSON 与额外字段。
	if err != nil && err != io.EOF {
		response.BadRequest(c, "请求字段或格式无效")
		return input, false
	}
	if err == nil && decoder.Decode(new(any)) != io.EOF {
		response.BadRequest(c, "请求格式无效")
		return input, false
	}
	input.Mode = strings.ToLower(strings.TrimSpace(input.Mode))
	if input.Mode != "" && input.Mode != service.AccountTestModeDefault && input.Mode != service.AccountTestModeCompact {
		response.BadRequest(c, "不支持的连接测试模式")
		return input, false
	}
	return input, true
}

func (h *SharedPoolHandler) allowAccountTest(userID int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	for key, at := range h.lastTest {
		if now.Sub(at) > time.Minute {
			delete(h.lastTest, key)
		}
	}
	if now.Sub(h.lastTest[userID]) < time.Minute {
		return false
	}
	if h.lastTest == nil {
		h.lastTest = map[int64]time.Time{}
	}
	h.lastTest[userID] = now
	return true
}
