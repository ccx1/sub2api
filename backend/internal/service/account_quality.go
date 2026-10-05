package service

import (
	"context"
	"fmt"
	"strings"
)

// QualityPolicy is opt-in. Legacy connectivity/HTML tests never modify membership.
const QualityActionRemoveModel = "remove_models"

type QualityPolicy struct {
	TriggerOnUpstream5xx bool                `json:"trigger_on_upstream_5xx"`
	RemoveModels         []string            `json:"remove_models,omitempty"`
	RecoveryConcurrency  int                 `json:"recovery_concurrency,omitempty"`
	Judge                *QualityJudgeConfig `json:"judge,omitempty"`
	ExpectedAnswer       string              `json:"expected_answer"`
	Action               string              `json:"action"`
	RemoveGroupIDs       []int64             `json:"remove_group_ids"`
	AutoRestore          bool                `json:"auto_restore"`
}

func validateQualityPolicy(plan *ScheduledTestPlan) error {
	q := plan.PelicanConfig.Quality
	if q == nil {
		return nil
	}
	if j := q.Judge; j != nil {
		if j.GroupID <= 0 || strings.TrimSpace(j.ModelID) == "" || len(j.ModelID) > 100 || strings.TrimSpace(j.Prompt) == "" || len(j.Prompt) > 16000 {
			return fmt.Errorf("judge group, model and grading prompt are required (100/16000 byte limits)")
		}
	}
	if plan.AutoRecover {
		return fmt.Errorf("quality plans use auto_restore, not connectivity auto_recover")
	}
	if plan.PelicanConfig.QuestionKind != "candy" {
		return fmt.Errorf("quality plans require a text answer question")
	}
	if strings.TrimSpace(q.ExpectedAnswer) == "" || len(q.ExpectedAnswer) > 4000 {
		return fmt.Errorf("expected answer must be 1–4000 bytes")
	}
	if q.Action != "remove_groups" && q.Action != "disable_scheduling" && q.Action != QualityActionRemoveModel {
		return fmt.Errorf("invalid quality action")
	}
	if q.RecoveryConcurrency < 0 || q.RecoveryConcurrency > 10000 {
		return fmt.Errorf("recovery concurrency must be 0-10000")
	}
	if q.TriggerOnUpstream5xx && q.Action != QualityActionRemoveModel {
		return fmt.Errorf("upstream 5xx protection requires model cooldown")
	}
	if q.Action == QualityActionRemoveModel {
		if len(q.RemoveModels) == 0 || len(q.RemoveModels) > 50 {
			return fmt.Errorf("select 1-50 cooldown models")
		}
		selected := map[string]bool{}
		for _, model := range qualityPlanModels(plan) {
			selected[model] = true
		}
		seenModels := map[string]bool{}
		for i, raw := range q.RemoveModels {
			model := strings.TrimSpace(raw)
			if !selected[model] || seenModels[model] || strings.ContainsAny(model, "*\r\n\t") {
				return fmt.Errorf("cooldown models must be unique selected test models")
			}
			q.RemoveModels[i], seenModels[model] = model, true
		}
		q.RemoveGroupIDs = nil
	} else {
		q.RemoveModels = nil
	}
	if q.Action == "remove_groups" && len(q.RemoveGroupIDs) == 0 {
		return fmt.Errorf("select at least one group to remove")
	}
	if len(q.RemoveGroupIDs) > 100 {
		return fmt.Errorf("select at most 100 groups")
	}
	seen := map[int64]bool{}
	for _, id := range q.RemoveGroupIDs {
		if id <= 0 || seen[id] {
			return fmt.Errorf("invalid or duplicate group ID")
		}
		seen[id] = true
	}
	return nil
}

// One completed wrong answer quarantines; restoration requires every probe to pass.
// Transport errors alone are inconclusive, never evidence of degradation.
func qualityOutcome(results []*ScheduledTestResult) string {
	allPassed := len(results) > 0
	for _, r := range results {
		if r != nil && r.QualityJudgment != nil && r.QualityJudgment.Verdict == "incorrect" && r.Status == "failed" && r.ErrorMessage == "answer_mismatch" {
			return "failed"
		}
		if r == nil || r.Status != "success" || r.QualityJudgment == nil || r.QualityJudgment.Verdict != "correct" {
			allPassed = false
		}
	}
	if allPassed {
		return "passed"
	}
	return "inconclusive"
}

func (s *ScheduledTestService) ListQualityPlans(ctx context.Context) ([]*ScheduledTestPlan, error) {
	return s.planRepo.ListQualityPlans(ctx)
}
func (s *ScheduledTestService) TriggerQuality(ctx context.Context, id int64) error {
	return s.planRepo.TriggerQuality(ctx, id)
}

func (s *ScheduledTestService) ListQualityHistory(ctx context.Context, beforeID int64) (*QualityHistoryPage, error) {
	items, err := s.resultRepo.ListQualityHistory(ctx, beforeID, 101)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		if item == nil {
			continue
		}
		expected := item.TotalCount
		if cfg := item.PelicanConfig; cfg != nil && len(cfg.ModelIDs) > 1 && len(cfg.ModelIDs)*cfg.ParallelCount > expected {
			expected = len(cfg.ModelIDs) * cfg.ParallelCount
		}
		item.TotalCount = expected - item.SkippedCount
		if item.TotalCount <= 0 {
			item.TotalCount, item.Status = 0, "skipped"
		} else if item.PassedCount == item.TotalCount {
			item.Status = "success"
		} else {
			item.Status = "failed"
		}
	}
	page := &QualityHistoryPage{Items: items}
	if len(items) > 100 {
		page.Items = items[:100]
		page.NextCursor = items[99].ID
	}
	return page, nil
}
