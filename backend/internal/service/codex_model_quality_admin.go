package service

import (
	"context"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

func (s *OpenAIGatewayService) GetCodexModelQualityStatuses(ctx context.Context, account *Account) ([]CodexModelQualityStatus, error) {
	return s.codexModelQualityStatuses(ctx, account, true)
}

// GetCodexModelQualityStatusSnapshot loads the latest persisted result without
// scheduling a new check. Account list responses use this path so merely
// rendering a usage window does not create background work for every account.
func (s *OpenAIGatewayService) GetCodexModelQualityStatusSnapshot(ctx context.Context, account *Account) ([]CodexModelQualityStatus, error) {
	return s.codexModelQualityStatuses(ctx, account, false)
}

func (s *OpenAIGatewayService) codexModelQualityStatuses(ctx context.Context, account *Account, schedule bool) ([]CodexModelQualityStatus, error) {
	if s == nil || s.settingService == nil {
		return nil, infraerrors.ServiceUnavailable("MODEL_QUALITY_UNAVAILABLE", "Model quality service unavailable")
	}
	policy, err := s.settingService.GetCodexModelQualityPolicy(ctx)
	if err != nil {
		return nil, err
	}
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	statuses := make([]CodexModelQualityStatus, 0, len(cfg.Models))
	store, available := s.accountRepo.(CodexModelQualityStore)
	models := prioritizedCodexModelQualityModels(cfg.Models, policy.ModelPriorities)
	for _, model := range models {
		status := CodexModelQualityStatus{AccountID: account.ID, Model: model, Status: "pending", Reason: "not_checked", ModelIdentity: "unknown", Source: "automatic"}
		if !available {
			status.Status, status.Reason = "skipped", "shared_state_unavailable"
			statuses = append(statuses, status)
			continue
		}
		record, err := store.LoadCodexModelQuality(ctx, account.ID, model)
		if err != nil {
			return nil, err
		}
		if record != nil {
			status = record.Status
			status.ConsecutiveLowQuality = record.ConsecutiveLowQuality
			status.QualityPausedUntil = record.QualityPausedUntil
		}
		ticket := s.lookupOpenAICodexTicketForConfig(account, model, cfg)
		if !policy.Enabled {
			// Disabling new checks must not erase the last persisted result. The
			// account list and history modal are still allowed to show that result.
			if record == nil {
				status.Status, status.Reason = "skipped", "disabled"
			}
		} else if record == nil && !ticket.usable(time.Now(), account, cfg) {
			status.Reason = "no_ticket"
		} else if record != nil {
			status = codexModelQualityDisplayStatus(record, account, ticket, cfg, policy)
		}
		if policy.Enabled && schedule {
			job, reason := s.prepareCodexModelQuality(ctx, account, model, policy)
			if job == nil && !(status.Status == "quarantined" && reason == "no_ticket") {
				status.Status, status.Reason, status.BaselineReused = "skipped", reason, false
				if record != nil {
					status.Status = "stale"
				}
			}
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

func (s *OpenAIGatewayService) RetryCodexModelQuality(ctx context.Context, account *Account, model string) (CodexModelQualityScheduleResult, error) {
	if s == nil || s.settingService == nil {
		return CodexModelQualityScheduleResult{}, infraerrors.ServiceUnavailable("MODEL_QUALITY_UNAVAILABLE", "Model quality service unavailable")
	}
	p, err := s.settingService.GetCodexModelQualityPolicy(ctx)
	if err != nil {
		return CodexModelQualityScheduleResult{}, err
	}
	return s.scheduleCodexModelQuality(ctx, account, strings.TrimSpace(model), "manual", p), nil
}
