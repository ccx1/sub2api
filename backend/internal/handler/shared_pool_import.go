package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

const (
	sharedImportMaxSources  = 20
	sharedImportMaxAccounts = 50
	sharedImportMaxContent  = 2 << 20
	sharedImportMaxBody     = 4 << 20
)

type sharedImportSource struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type sharedImportDefaults struct {
	Name               string                           `json:"name"`
	Platform           string                           `json:"platform"`
	Type               string                           `json:"type"`
	Concurrency        int                              `json:"concurrency"`
	ProxyURL           *string                          `json:"proxy_url"`
	Enabled            bool                             `json:"enabled"`
	DispatchConsent    bool                             `json:"dispatch_consent"`
	ProtectionEnabled  bool                             `json:"protection_enabled"`
	CodexTicketEnabled *bool                            `json:"codex_ticket_enabled"`
	DailyCooldown      *service.SharedPoolDailyCooldown `json:"daily_cooldown,omitempty"`
}

type sharedImportRequest struct {
	Sources  []sharedImportSource `json:"sources"`
	Defaults sharedImportDefaults `json:"defaults"`
}

type sharedImportItem struct {
	Index     int    `json:"index"`
	Source    string `json:"source"`
	Name      string `json:"name"`
	AccountID int64  `json:"account_id,omitempty"`
	Message   string `json:"message,omitempty"`
}

type sharedImportResult struct {
	Total    int                `json:"total"`
	Created  int                `json:"created"`
	Failed   int                `json:"failed"`
	Items    []sharedImportItem `json:"items"`
	Warnings []string           `json:"warnings"`
}

type sharedImportEntry struct {
	item     sharedImportItem
	input    service.SharedPoolAccountInput
	warnings []string
}

var sharedImportUsers sync.Map

func (h *SharedPoolHandler) ImportAccounts(c *gin.Context) {
	userID, ok := sharedUser(c)
	if !ok {
		return
	}
	var req sharedImportRequest
	if !bindSharedImport(c, &req) {
		return
	}
	entries, err := parseSharedImport(req)
	if err != nil {
		sharedReply(c, nil, err)
		return
	}
	executeSharedImportIdempotent(c, userID, req, func(ctx context.Context) (any, error) {
		return executeSharedImport(ctx, userID, entries, h.pool.Create)
	})
}

func executeSharedImportIdempotent(c *gin.Context, userID int64, req sharedImportRequest, execute func(context.Context) (any, error)) {
	coordinator := service.DefaultIdempotencyCoordinator()
	if coordinator == nil {
		data, err := execute(c.Request.Context())
		sharedReply(c, data, err)
		return
	}
	// 导入可能超过普通写入的 30 秒租约，启用续租以免重试接管仍在执行的批次。
	actor := "user:" + strconv.FormatInt(userID, 10)
	result, err := coordinator.Execute(c.Request.Context(), service.IdempotencyExecuteOptions{
		Scope: "user.shared_pool.import:" + actor, ActorScope: actor,
		Method: c.Request.Method, Route: c.FullPath(), IdempotencyKey: c.GetHeader("Idempotency-Key"),
		Payload: req, RequireKey: true, TTL: service.DefaultWriteIdempotencyTTL(), ExecutionTimeout: 280 * time.Second,
	}, execute)
	if err != nil {
		if seconds := service.RetryAfterSecondsFromError(err); seconds > 0 {
			c.Header("Retry-After", strconv.Itoa(seconds))
		}
		sharedReply(c, nil, err)
		return
	}
	if result.Replayed {
		c.Header("X-Idempotency-Replayed", "true")
	}
	sharedReply(c, result.Data, nil)
}

func bindSharedImport(c *gin.Context, req *sharedImportRequest) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, sharedImportMaxBody)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(req); err != nil || decoder.Decode(new(any)) != io.EOF {
		response.BadRequest(c, "导入请求格式无效或超过大小限制")
		return false
	}
	return true
}

type sharedImportCreate func(context.Context, int64, service.SharedPoolAccountInput) (*service.SharedPoolAccountView, error)

func executeSharedImport(ctx context.Context, userID int64, entries []sharedImportEntry, create sharedImportCreate) (*sharedImportResult, error) {
	if _, busy := sharedImportUsers.LoadOrStore(userID, true); busy {
		return nil, infraerrors.New(http.StatusTooManyRequests, "SHARED_IMPORT_BUSY", "已有账号导入正在进行，请稍后重试")
	}
	defer sharedImportUsers.Delete(userID)
	ctx, cancel := context.WithTimeout(ctx, 280*time.Second)
	defer cancel()
	result := &sharedImportResult{Total: len(entries), Items: make([]sharedImportItem, len(entries)), Warnings: []string{}}
	jobs := make(chan int, len(entries))
	for i, entry := range entries {
		result.Items[i] = entry.item
		result.Warnings = append(result.Warnings, entry.warnings...)
		if entry.item.Message == "" {
			jobs <- i
		}
	}
	close(jobs)
	var workers sync.WaitGroup
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for index := range jobs {
				createSharedImportItem(ctx, userID, entries[index].input, &result.Items[index], create)
			}
		}()
	}
	workers.Wait()
	boundSharedImportResult(result)
	for _, item := range result.Items {
		if item.AccountID > 0 {
			result.Created++
		} else {
			result.Failed++
		}
	}
	return result, nil
}

func createSharedImportItem(ctx context.Context, userID int64, input service.SharedPoolAccountInput, item *sharedImportItem, create sharedImportCreate) {
	if ctx.Err() != nil {
		item.Message = "导入已取消或超时，请刷新账号列表后重试"
		return
	}
	account, err := create(ctx, userID, input)
	if err != nil {
		item.Message = "账号导入失败，请稍后重试"
		if infraerrors.Code(err) < 500 {
			item.Message = infraerrors.Message(err)
		}
		return
	}
	if account == nil || account.ID <= 0 {
		item.Message = "账号导入结果异常，请刷新账号列表"
		return
	}
	item.AccountID = account.ID
	item.Name = account.Name
}
