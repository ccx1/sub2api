package service

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// 手动与自动入口共用本实例并发上限，跨实例账号准入由 Redis 租约保证。
func (s *OpenAIGatewayService) acquireCodexTicketWorker(ctx context.Context) bool {
	limit := int32(max(1, s.openAICodexTicketConfigContext(ctx).HarvestConcurrency))
	for ctx.Err() == nil {
		current := s.openaiCodexTicketActive.Load()
		if current >= limit {
			return false
		}
		if s.openaiCodexTicketActive.CompareAndSwap(current, current+1) {
			return true
		}
	}
	return false
}

func (s *OpenAIGatewayService) acquireCodexTicketWorkerForProbe(ctx context.Context, manual bool) bool {
	if !manual {
		return s.acquireCodexTicketWorker(ctx)
	}
	for ctx.Err() == nil {
		if s.acquireCodexTicketWorker(ctx) {
			return true
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return false
		case <-timer.C:
		}
	}
	return false
}

func (s *OpenAIGatewayService) orderedCodexTicketModels(ctx context.Context, accountID int64, models []string) []string {
	ordered := slices.Clone(models)
	slices.Sort(ordered)
	status, err := s.GetOpenAICodexTicketRuntimeStatus(ctx, accountID)
	if err != nil || status == nil {
		return ordered
	}
	if index := slices.Index(ordered, status.LastModel); index >= 0 {
		ordered = append(ordered[index+1:], ordered[:index+1]...)
	}
	return ordered
}

func (s *OpenAIGatewayService) refreshScheduledCodexTickets(ctx context.Context, accounts []Account, cfg config.OpenAICodexTicketConfig) {
	var workers sync.WaitGroup
	slots := make(chan struct{}, max(1, cfg.HarvestConcurrency))
	for i := range accounts {
		account := &accounts[i]
		if !openAICodexTicketHarvestEnabled(account) || account.IsInDailyCooldown(time.Now()) {
			continue
		}
		models := s.codexTicketPendingModels(ctx, account, cfg)
		if len(models) == 0 {
			continue
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			workers.Wait()
			return
		}
		workers.Add(1)
		go func(account *Account, models []string) {
			defer workers.Done()
			defer func() { <-slots }()
			for _, model := range s.orderedCodexTicketModels(ctx, account.ID, models) {
				if ctx.Err() != nil {
					return
				}
				if s.codexModelQualityCircuitPaused(ctx, account, model) {
					continue
				}
				s.probeOnceOpenAICodexTicket(ctx, cloneOpenAICodexTicketAccount(account), model)
			}
		}(cloneOpenAICodexTicketAccount(account), models)
	}
	workers.Wait()
}

func (s *OpenAIGatewayService) retryScheduledCodexTickets(ctx context.Context, account *Account, models []string, cfg config.OpenAICodexTicketConfig) CodexTicketRetryResult {
	result := CodexTicketRetryResult{Models: []string{}}
	for _, model := range models {
		if s.codexModelQualityCircuitPaused(ctx, account, model) {
			result.Skipped++
			continue
		}
		result.Models = append(result.Models, model)
	}
	result.Scheduled = len(result.Models)
	if result.Scheduled > 0 {
		manualCtx := withCodexTicketManualRetry(context.WithoutCancel(ctx))
		s.submitManualCodexTicketProbes(manualCtx, account, s.orderedCodexTicketModels(manualCtx, account.ID, result.Models))
	}
	return result
}
