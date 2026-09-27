package service

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// 某个 API Key 账号的单个模型持续返回 503（如 "Service temporarily unavailable"）
// 时的处理链路：
//  1. 请求路径：写入 (账号, 模型) 级冷却，reason=upstreamModelUnavailableReason，
//     调度器通过 IsSchedulableForModelWithContext 跳过该模型，账号的其它模型不受影响；
//  2. 后台 ModelAvailabilityRecheckService：冷却到期后用该模型做一次真实测试，
//     成功则解除冷却，失败则从账号 credentials.model_mapping 中删除该模型。
const (
	upstreamModelUnavailableCooldown = 5 * time.Minute
	upstreamModelUnavailableReason   = "upstream_503_model_unavailable"
)

// ModelAvailabilityRepository 是模型可用性复检所需的最小持久化能力。
// 由 repository.accountRepository 实现；单独成接口以免扩大 AccountRepository，
// 进而牵动大量嵌入该接口的测试桩。
type ModelAvailabilityRepository interface {
	ListModelRateLimitedAccountIDsByReason(ctx context.Context, reason string, limit int) ([]int64, error)
	ClearModelRateLimitIfReason(ctx context.Context, id int64, scope, reason string) (bool, error)
	RemoveModelMappingKeys(ctx context.Context, id int64, keys []string) (bool, error)
}

// shouldCooldownUpstreamModelUnavailable 判断一次 503 是否应视为"该账号上的该模型不可用"。
// 只处理 API Key 账号（OAuth 账号的 503 多为官方侧整体抖动，交给既有瞬时退避）；
// 请求级容量降载（server is overloaded / slow_down）不是模型失效，必须排除。
func shouldCooldownUpstreamModelUnavailable(account *Account, statusCode int, responseBody []byte) bool {
	if account == nil || account.Type != AccountTypeAPIKey || statusCode != http.StatusServiceUnavailable {
		return false
	}
	if account.Platform == PlatformAntigravity {
		// Antigravity 的 503 有独立的模型容量/智能重试链路。
		return false
	}
	if account.IsPoolMode() {
		// 池模式上游自带多路由，503 不代表模型本身不可用；
		// 且池模式同账号重试预算需要保持可用。
		return false
	}
	if !account.ShouldHandleErrorCode(statusCode) {
		return false
	}
	if isOpenAIRequestScopedCapacityShed("", responseBody) || isOpenAITransientProcessingError(statusCode, "", responseBody) {
		return false
	}
	return true
}

// HandleUpstreamModelUnavailable 在 API Key 账号对某模型返回 503 时写入模型级冷却。
// 返回 true 表示已写入（或尝试写入）冷却，调用方应换号重试。
func (s *RateLimitService) HandleUpstreamModelUnavailable(ctx context.Context, account *Account, requestedModel string, statusCode int, responseBody []byte) bool {
	if s == nil || s.accountRepo == nil || !shouldCooldownUpstreamModelUnavailable(account, statusCode, responseBody) {
		return false
	}
	modelKey := modelRateLimitKeyForUpstreamModelNotFound(ctx, account, requestedModel)
	if modelKey == "" {
		return false
	}
	now := time.Now()
	resetAt := now.Add(upstreamModelUnavailableCooldown)
	if err := s.accountRepo.SetModelRateLimit(ctx, account.ID, modelKey, resetAt, upstreamModelUnavailableReason); err != nil {
		slog.Warn("upstream_model_unavailable_set_model_rate_limit_failed", "account_id", account.ID, "model", modelKey, "error", err)
		return true
	}
	// 同步本地快照，保证同一请求的后续换号判断立即生效。
	setAccountModelRateLimitSnapshot(account, modelKey, resetAt, upstreamModelUnavailableReason, now)
	slog.Warn("upstream_model_unavailable_model_rate_limited",
		"account_id", account.ID,
		"model", modelKey,
		"reset_at", resetAt,
		"upstream_message", truncateForLog([]byte(strings.TrimSpace(extractUpstreamErrorMessage(responseBody))), 256),
	)
	return true
}
