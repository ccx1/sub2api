package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQualityModelCatalogUnavailableSkipsWithoutAccountAction(t *testing.T) {
	plans, results := &qualityPlanRepo{}, &pelicanResults{}
	svc := NewScheduledTestService(plans, results)
	svc.qualityModels = func(context.Context, int64) ([]string, error) { return nil, errors.New("discovery unavailable") }
	runner := &ScheduledTestRunnerService{planRepo: plans, scheduledSvc: svc}
	runner.runPelican = func(context.Context, int64, string, *PelicanTestConfig) (*ScheduledTestResult, error) {
		t.Fatal("catalog failure must not invoke models")
		return nil, nil
	}
	runner.runOnePlan(context.Background(), multiQualityPlan())
	require.Equal(t, []string{"inconclusive"}, plans.outcomes)
	require.Len(t, results.results, 2)
	for _, result := range results.results {
		require.Equal(t, "skipped", result.Status)
	}
}

func TestQualityModelCatalogFiltersUnsupportedModel(t *testing.T) {
	plans, results := &qualityPlanRepo{}, &pelicanResults{}
	svc := NewScheduledTestService(plans, results)
	svc.qualityModels = func(context.Context, int64) ([]string, error) { return []string{"gpt-6-astra"}, nil }
	runner := &ScheduledTestRunnerService{planRepo: plans, scheduledSvc: svc}
	runner.runPelican = func(_ context.Context, _ int64, model string, _ *PelicanTestConfig) (*ScheduledTestResult, error) {
		require.Equal(t, "gpt-6-astra", model)
		return &ScheduledTestResult{Status: "success", ResponseText: "21"}, nil
	}
	runner.judgeQuality = func(context.Context, int64, *PelicanTestConfig, string) *QualityJudgment {
		return &QualityJudgment{Verdict: "correct"}
	}
	runner.runOnePlan(context.Background(), multiQualityPlan())
	require.Equal(t, "success", results.results[0].Status)
	require.Equal(t, "skipped", results.results[1].Status)
	require.Equal(t, "skipped", results.results[1].PelicanConfig.QualityModelOutcomes["gpt-6-sol"])
}

type modelQualityHistoryRepository struct {
	ScheduledTestResultRepository
	items []*QualityHistoryResult
}

func (r *modelQualityHistoryRepository) ListQualityHistory(context.Context, int64, int) ([]*QualityHistoryResult, error) {
	return r.items, nil
}

func TestQualityModelHistoryIncompleteRoundIsNotPassing(t *testing.T) {
	partial := &QualityHistoryResult{PassedCount: 1, TotalCount: 1}
	partial.PelicanConfig = multiQualityPlan().PelicanConfig
	skipped := &QualityHistoryResult{TotalCount: 2, SkippedCount: 2}
	skipped.PelicanConfig = multiQualityPlan().PelicanConfig
	svc := NewScheduledTestService(nil, &modelQualityHistoryRepository{items: []*QualityHistoryResult{partial, skipped}})
	page, err := svc.ListQualityHistory(context.Background(), 0)
	require.NoError(t, err)
	require.Equal(t, 2, page.Items[0].TotalCount)
	require.Equal(t, "failed", page.Items[0].Status)
	require.Zero(t, page.Items[1].TotalCount)
	require.Equal(t, "skipped", page.Items[1].Status)
}
