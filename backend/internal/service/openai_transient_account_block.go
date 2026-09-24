package service

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

const (
	openAITransientAccountFailureThreshold = 3
	openAITransientAccountCooldown         = 2 * time.Minute
	openAITransientAccountBlockReason      = "openai_transient_503"
	// 账号级 503 计数使用独立键，跨模型汇总，但不混入模型级 500/502 计数。
	openAITransientAccountFailureModelKey = "__account_transient_503__"
)

func (s *OpenAIGatewayService) recordOpenAITransient503Failure(account *Account, now time.Time) openAIAccountModelTransientDecision {
	if s == nil || account == nil {
		return openAIAccountModelTransientDecision{}
	}
	return s.recordOpenAIAccountModelTransientFailure(account, openAITransientAccountFailureModelKey, now)
}

func (s *OpenAIGatewayService) clearOpenAITransient503Failure(accountID int64) {
	if s == nil || accountID <= 0 {
		return
	}
	s.clearOpenAIAccountModelTransientState(accountID, openAITransientAccountFailureModelKey)
}

// blockOpenAIAccountAfterTransientFailure 在连续 503 达到阈值后临时停调账号。
// 调用前已排除请求级容量降载，避免因为共享上游过载而误停账号。
func (s *OpenAIGatewayService) blockOpenAIAccountAfterTransientFailure(
	ctx context.Context,
	account *Account,
	statusCode int,
	responseBody []byte,
	decision openAIAccountModelTransientDecision,
) {
	if s == nil || s.accountRepo == nil || account == nil ||
		account.Platform != PlatformOpenAI || account.Type != AccountTypeAPIKey ||
		statusCode != http.StatusServiceUnavailable ||
		decision.FailureStreak < openAITransientAccountFailureThreshold {
		return
	}

	now := time.Now()
	if account.TempUnschedulableUntil != nil && now.Before(*account.TempUnschedulableUntil) {
		return
	}
	until := now.Add(openAITransientAccountCooldown)
	state := &TempUnschedState{
		UntilUnix:            until.Unix(),
		TriggeredAtUnix:      now.Unix(),
		StatusCode:           statusCode,
		MatchedKeyword:       openAITransientAccountBlockReason,
		RuleIndex:            -1,
		ErrorMessage:         truncateTempUnschedMessage(responseBody, tempUnschedMessageMaxBytes),
		TriggerCount:         int64(decision.FailureStreak),
		TriggerThreshold:     openAITransientAccountFailureThreshold,
		TriggerWindowMinutes: int(openAIModelTransientStreakTTL / time.Minute),
	}
	reasonBytes, _ := json.Marshal(state)
	reason := string(reasonBytes)
	if reason == "" {
		reason = openAITransientAccountBlockReason
	}

	persistCtx := ctx
	if persistCtx == nil {
		persistCtx = context.Background()
	}
	if err := s.accountRepo.SetTempUnschedulable(persistCtx, account.ID, until, reason); err != nil {
		slog.Warn("openai.transient_503_set_temp_unschedulable_failed",
			"account_id", account.ID, "failure_streak", decision.FailureStreak, "error", err)
		return
	}
	account.TempUnschedulableUntil = &until
	account.TempUnschedulableReason = reason
	if s.rateLimitService != nil {
		s.rateLimitService.notifyAccountSchedulingBlocked(account, until, openAITransientAccountBlockReason)
		if s.rateLimitService.tempUnschedCache != nil {
			if err := s.rateLimitService.tempUnschedCache.SetTempUnsched(persistCtx, account.ID, state); err != nil {
				slog.Warn("openai.transient_503_set_temp_unschedulable_cache_failed", "account_id", account.ID, "error", err)
			}
		}
	}
	slog.Warn("openai.transient_503_account_paused",
		"account_id", account.ID,
		"failure_streak", decision.FailureStreak,
		"cooldown", openAITransientAccountCooldown,
		"until", until,
	)
}
