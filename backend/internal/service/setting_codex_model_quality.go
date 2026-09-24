package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const SettingKeyCodexModelQuality = "openai_codex_model_quality_policy"

func DefaultCodexModelQualityPolicy() CodexModelQualityPolicy {
	return CodexModelQualityPolicy{IntervalSeconds: 3600, TimeoutSeconds: 30,
		ReserveSeconds: 30, MaxTTLPercent: 10, Concurrency: 1, AccountConcurrency: 1, RetryIntervalSeconds: 300,
		LowQualityConsecutiveThreshold: 3, LowQualityCooldownSeconds: 900, ReplacementCheckDelaySeconds: 300,
		QuarantineOnFailure: true, FingerprintEnabled: true, ReasoningEffort: "low"}
}

func validateCodexModelQualityPolicy(p CodexModelQualityPolicy) error {
	limits := []struct {
		name            string
		value, min, max int
	}{
		{"interval_seconds", p.IntervalSeconds, 300, 86400},
		{"timeout_seconds", p.TimeoutSeconds, 5, 120},
		{"reserve_seconds", p.ReserveSeconds, 5, 600},
		{"max_ttl_percent", p.MaxTTLPercent, 1, 25},
		{"concurrency", p.Concurrency, 1, 64},
		{"account_concurrency", p.AccountConcurrency, 1, 64},
		{"retry_interval_seconds", p.RetryIntervalSeconds, 60, 3600},
		{"low_quality_consecutive_threshold", p.LowQualityConsecutiveThreshold, 1, 100},
		{"low_quality_cooldown_seconds", p.LowQualityCooldownSeconds, 60, 86400},
		{"replacement_check_delay_seconds", p.ReplacementCheckDelaySeconds, 60, 86400},
	}
	for _, limit := range limits {
		if limit.value < limit.min || limit.value > limit.max {
			return infraerrors.BadRequest("INVALID_MODEL_QUALITY_POLICY", fmt.Sprintf("%s must be between %d and %d", limit.name, limit.min, limit.max))
		}
	}
	if p.ReasoningEffort != "low" && p.ReasoningEffort != "medium" && p.ReasoningEffort != "high" {
		return infraerrors.BadRequest("INVALID_MODEL_QUALITY_POLICY", "reasoning_effort must be low, medium or high")
	}
	if len(p.ModelPriorities) > 128 {
		return infraerrors.BadRequest("INVALID_MODEL_QUALITY_POLICY", "model_priorities contains too many models")
	}
	for model, priority := range p.ModelPriorities {
		if model == "" || model != strings.TrimSpace(model) || len(model) > 160 {
			return infraerrors.BadRequest("INVALID_MODEL_QUALITY_POLICY", "model_priorities contains an invalid model")
		}
		if priority < 0 || priority > 100 {
			return infraerrors.BadRequest("INVALID_MODEL_QUALITY_POLICY", fmt.Sprintf("model priority for %s must be between 0 and 100", model))
		}
	}
	return nil
}

func (s *SettingService) GetCodexModelQualityPolicy(ctx context.Context) (CodexModelQualityPolicy, error) {
	p := DefaultCodexModelQualityPolicy()
	if s == nil || s.settingRepo == nil {
		return p, infraerrors.ServiceUnavailable("MODEL_QUALITY_UNAVAILABLE", "Model quality settings unavailable")
	}
	raw, err := s.settingRepo.GetValue(ctx, SettingKeyCodexModelQuality)
	if errors.Is(err, ErrSettingNotFound) || err == nil && raw == "" {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	if !strings.HasPrefix(strings.TrimSpace(raw), "{") {
		return DefaultCodexModelQualityPolicy(), errors.New("invalid stored model quality policy")
	}
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return DefaultCodexModelQualityPolicy(), errors.New("invalid stored model quality policy")
	}
	return p, validateCodexModelQualityPolicy(p)
}

func (s *SettingService) UpdateCodexModelQualityPolicy(ctx context.Context, p CodexModelQualityPolicy) (CodexModelQualityPolicy, error) {
	if err := validateCodexModelQualityPolicy(p); err != nil {
		return p, err
	}
	if s == nil || s.settingRepo == nil {
		return p, infraerrors.ServiceUnavailable("MODEL_QUALITY_UNAVAILABLE", "Model quality settings unavailable")
	}
	raw, err := json.Marshal(p)
	if err == nil {
		s.codexTicketPublishMu.Lock()
		defer s.codexTicketPublishMu.Unlock()
		err = s.settingRepo.SetMultiple(ctx, map[string]string{SettingKeyCodexModelQuality: string(raw)})
	}
	return p, err
}
