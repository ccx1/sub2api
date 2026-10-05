package service

import (
	"context"
	"sync"
	"time"
)

func (s *ScheduledTestRunnerService) runQualitySamples(ctx context.Context, plan *ScheduledTestPlan) []*ScheduledTestResult {
	models := qualityPlanModels(plan)
	results := make([]*ScheduledTestResult, len(models)*plan.PelicanConfig.ParallelCount)
	catalog, catalogErr := s.scheduledSvc.supportedQualityModels(ctx, plan)
	jobs := make(chan int, len(results))
	for index := range results {
		jobs <- index
	}
	close(jobs)
	workers := min(len(results), 8)
	var unsupported sync.Map
	var wg sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				sample := *plan
				cfg := *plan.PelicanConfig
				sample.ModelID, cfg.ModelID = models[index%len(models)], models[index%len(models)]
				sample.PelicanConfig = &cfg
				_, excluded := unsupported.Load(sample.ModelID)
				if cfg.Quality != nil && (catalogErr != nil || excluded || !qualityModelListed(catalog, sample.ModelID)) {
					reason := "model_unsupported"
					if catalogErr != nil {
						reason = "model_catalog_unavailable"
					}
					results[index] = &ScheduledTestResult{Status: "skipped", ErrorMessage: reason, StartedAt: time.Now(), FinishedAt: time.Now(), PelicanConfig: &cfg}
					continue
				}
				result := s.runPelicanSample(ctx, &sample)
				result.PelicanConfig = &cfg
				if cfg.Quality != nil && qualityModelUnsupported(result.ErrorMessage) {
					unsupported.Store(sample.ModelID, true)
					result.Status, result.ErrorMessage, result.QualityJudgment = "skipped", "model_unsupported", nil
				}
				results[index] = result
			}
		}()
	}
	wg.Wait()
	return results
}
