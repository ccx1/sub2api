package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func multiQualityPlan() *ScheduledTestPlan {
	p := pelicanPlan()
	p.PelicanConfig.QuestionKind = "candy"
	p.PelicanConfig.ParallelCount = 1
	p.PelicanConfig.ModelIDs = []string{"gpt-6-astra", "gpt-6-sol"}
	p.PelicanConfig.Quality = &QualityPolicy{ExpectedAnswer: "21", Action: QualityActionRemoveModel, RemoveModels: []string{"gpt-6-astra", "gpt-6-sol"}}
	return p
}

func TestQualityMultiModelValidationAndLegacy(t *testing.T) {
	p := multiQualityPlan()
	_, err := nextPlanRun(p, time.Now())
	require.NoError(t, err)
	require.False(t, p.PelicanConfig.Quality.TriggerOnUpstream5xx)
	p.MaxResults = 1
	_, err = nextPlanRun(p, time.Now())
	require.Error(t, err)
	p = multiQualityPlan()
	p.PelicanConfig.ModelIDs = []string{"a", " "}
	_, err = nextPlanRun(p, time.Now())
	require.Error(t, err)
	legacy := pelicanPlan()
	_, err = nextPlanRun(legacy, time.Now())
	require.NoError(t, err)
}

func TestQualityMultiModelTransportIsNotDegradation(t *testing.T) {
	passed := &ScheduledTestResult{Status: "success", QualityJudgment: &QualityJudgment{Verdict: "correct"}}
	transport := &ScheduledTestResult{Status: "failed", ErrorMessage: "timeout"}
	skipped := &ScheduledTestResult{Status: "skipped"}
	require.Equal(t, "inconclusive", qualityRoundOutcome([]*ScheduledTestResult{passed, transport}))
	require.Equal(t, "passed", qualityRoundOutcome([]*ScheduledTestResult{passed, skipped}))
	require.Equal(t, "inconclusive", qualityRoundOutcome([]*ScheduledTestResult{skipped}))
}

func TestQualityMultiModelWaitsForEveryModel(t *testing.T) {
	plans, results := &qualityPlanRepo{}, &pelicanResults{}
	runner := &ScheduledTestRunnerService{planRepo: plans, scheduledSvc: NewScheduledTestService(plans, results)}
	started, release := make(chan string, 2), make(chan struct{})
	runner.runPelican = func(ctx context.Context, _ int64, model string, cfg *PelicanTestConfig) (*ScheduledTestResult, error) {
		started <- model
		if model == "gpt-6-sol" {
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return nil, errors.New("upstream timeout")
		}
		return &ScheduledTestResult{Status: "success", ResponseText: "21", PelicanConfig: cfg}, nil
	}
	runner.judgeQuality = func(context.Context, int64, *PelicanTestConfig, string) *QualityJudgment {
		return &QualityJudgment{Verdict: "correct"}
	}
	done := make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	go func() { runner.runOnePlan(ctx, multiQualityPlan()); close(done) }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("both models must start")
		}
	}
	select {
	case <-done:
		t.Fatal("round finished before the slow model")
	default:
	}
	close(release)
	<-done
	require.Equal(t, []string{"inconclusive"}, plans.outcomes)
	require.Len(t, results.results, 2)
	require.Equal(t, results.results[0].QualityRoundID, results.results[1].QualityRoundID)
	require.Equal(t, "gpt-6-astra", results.results[0].PelicanConfig.ModelID)
	require.Equal(t, "gpt-6-sol", results.results[1].PelicanConfig.ModelID)
}
