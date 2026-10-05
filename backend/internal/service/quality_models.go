package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

func qualityPlanModels(plan *ScheduledTestPlan) []string {
	if plan.PelicanConfig != nil && len(plan.PelicanConfig.ModelIDs) > 0 {
		return plan.PelicanConfig.ModelIDs
	}
	return []string{plan.ModelID}
}

func normalizeQualityPlanModels(plan *ScheduledTestPlan) error {
	cfg := plan.PelicanConfig
	if len(cfg.ModelIDs) == 0 {
		return nil
	}
	if cfg.Quality == nil || len(cfg.ModelIDs) > 50 {
		return fmt.Errorf("multiple models require a quality rule with at most 50 models")
	}
	models, seen := make([]string, 0, len(cfg.ModelIDs)), map[string]bool{}
	for _, raw := range cfg.ModelIDs {
		model := strings.TrimSpace(raw)
		if model == "" || len(model) > 100 || strings.ContainsAny(model, "\r\n\t") {
			return fmt.Errorf("quality model must be 1-100 bytes")
		}
		if !seen[model] {
			models = append(models, model)
			seen[model] = true
		}
	}
	cfg.ModelIDs, plan.ModelID = models, models[0]
	return nil
}

// Quality discovery shares the account test catalog and never changes tickets.
func (s *ScheduledTestService) ConfigureQualityModels(tests *AccountTestService) {
	s.accountTests = tests
	if tests != nil {
		s.qualityModels = s.accountQualityModels
	}
}

func (s *ScheduledTestService) accountQualityModels(ctx context.Context, id int64) ([]string, error) {
	account, err := s.accountTests.accountRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, ErrAccountNotFound
	}
	models := []string{}
	if account.IsOpenAI() {
		catalog, err := s.accountTests.FetchOpenAIAccountModels(ctx, account)
		if err != nil {
			return nil, err
		}
		for _, model := range catalog {
			models = append(models, model.ID)
		}
		return models, nil
	}
	if mapping := account.GetModelMapping(); len(mapping) > 0 {
		for model := range mapping {
			models = append(models, model)
		}
		return models, nil
	}
	switch account.Platform {
	case PlatformGemini:
		catalog := geminicli.DefaultModels
		if account.IsGeminiGoogleOne() {
			catalog = geminicli.GoogleOneModels
		}
		for _, model := range catalog {
			models = append(models, model.ID)
		}
	case PlatformGrok:
		for _, model := range xai.DefaultModels() {
			models = append(models, model.ID)
		}
	case PlatformAntigravity:
		for _, model := range antigravity.DefaultModels() {
			models = append(models, model.ID)
		}
	default:
		if saved := account.GetUpstreamModelMetadataSnapshot(); saved != nil {
			for model := range saved.Models {
				models = append(models, model)
			}
		} else if account.Platform == PlatformAnthropic {
			for _, model := range claude.DefaultModels {
				models = append(models, model.ID)
			}
		} else {
			return s.accountTests.FetchUpstreamSupportedModels(ctx, account)
		}
	}
	return models, nil
}

func qualityModelListed(catalog []string, model string) bool {
	for _, candidate := range catalog {
		if candidate == model || strings.Contains(candidate, "*") && matchWildcard(candidate, model) {
			return true
		}
	}
	return false
}

func (s *ScheduledTestService) supportedQualityModels(ctx context.Context, plan *ScheduledTestPlan) ([]string, error) {
	if plan.PelicanConfig.Quality == nil || s.qualityModels == nil {
		return qualityPlanModels(plan), nil
	}
	return s.qualityModels(ctx, plan.AccountID)
}

func (s *ScheduledTestService) validateQualityAccountModels(ctx context.Context, plan *ScheduledTestPlan) error {
	if plan.PelicanConfig == nil || plan.PelicanConfig.Quality == nil || s.qualityModels == nil {
		return nil
	}
	catalog, err := s.supportedQualityModels(ctx, plan)
	if err != nil {
		return fmt.Errorf("quality model catalog unavailable: %w", err)
	}
	for _, model := range qualityPlanModels(plan) {
		if !qualityModelListed(catalog, model) {
			return fmt.Errorf("account #%d does not support quality model %s", plan.AccountID, model)
		}
	}
	return nil
}

func qualityModelUnsupported(message string) bool {
	lower := strings.ToLower(message)
	for _, code := range []string{"model_not_found", "model_not_supported", "unsupported_model", "model_unsupported"} {
		if strings.Contains(lower, code) {
			return true
		}
	}
	return strings.Contains(lower, "model") && (strings.Contains(lower, "does not exist") || strings.Contains(lower, "model is not supported"))
}

func qualityRoundOutcome(results []*ScheduledTestResult) string {
	effective := make([]*ScheduledTestResult, 0, len(results))
	for _, result := range results {
		if result == nil || result.Status != "skipped" {
			effective = append(effective, result)
		}
	}
	return qualityOutcome(effective)
}

func qualityModelOutcomes(results []*ScheduledTestResult, targets []string) map[string]string {
	outcomes := make(map[string]string, len(targets))
	for _, model := range targets {
		samples := []*ScheduledTestResult{}
		for _, result := range results {
			if result != nil && result.PelicanConfig != nil && result.PelicanConfig.ModelID == model {
				samples = append(samples, result)
			}
		}
		outcomes[model] = qualityRoundOutcome(samples)
		allSkipped := len(samples) > 0
		for _, result := range samples {
			allSkipped = allSkipped && result.Status == "skipped"
		}
		if allSkipped {
			outcomes[model] = "skipped"
		}
	}
	return outcomes
}
