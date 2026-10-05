package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
)

func historicalQualityExtensionDue(ticket *openAICodexTicket, cfg config.OpenAICodexTicketConfig, now time.Time) bool {
	if ticket == nil || !cfg.HistoricalQualityEnabled || !config.CodexTicketUsageAgedEnabled(cfg) {
		return false
	}
	expires := ticket.historicalExpires(cfg)
	if !expires.After(now) || expires.After(now.Add(time.Duration(cfg.HistoricalQualityCheckBeforeSeconds)*time.Second)) {
		return false
	}
	return ticket.HistoricalQualityCheckedAt.IsZero() ||
		!now.Before(ticket.HistoricalQualityCheckedAt.Add(time.Duration(cfg.HistoricalQualityCheckIntervalSeconds)*time.Second))
}

func (s *OpenAIGatewayService) scheduleHistoricalQualityExtension(ctx context.Context, account *Account, model string, policy CodexModelQualityPolicy) {
	if !policy.Enabled || account == nil {
		return
	}
	job, _ := s.prepareCodexModelQuality(ctx, account, model, policy)
	if job == nil || !historicalQualityExtensionDue(job.ticket, job.config, time.Now()) {
		return
	}
	store, ok := s.accountRepo.(CodexModelQualityStore)
	if !ok {
		return
	}
	runCtx, acquired, _ := s.takeCodexModelQualityWorker(account.ID, policy.Concurrency, policy.AccountConcurrency)
	if !acquired {
		return
	}
	job.lease, job.source = uuid.NewString(), "historical_renewal"
	leaseTTL := time.Duration(policy.TimeoutSeconds+15) * time.Second
	job.leaseExpiresAt = time.Now().Add(leaseTTL)
	locked, err := store.AcquireCodexModelQuality(ctx, account.ID, model, job.lease, leaseTTL)
	if err != nil || !locked {
		s.finishCodexModelQualityWorker(account.ID)
		return
	}
	current, _ := s.prepareCodexModelQuality(ctx, account, model, policy)
	if current == nil || !sameCodexTicketLineage(current.ticket, job.ticket) ||
		!historicalQualityExtensionDue(current.ticket, current.config, time.Now()) {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = store.ReleaseCodexModelQuality(releaseCtx, account.ID, model, job.lease)
		cancel()
		s.finishCodexModelQualityWorker(account.ID)
		return
	}
	job.ticket, job.config = current.ticket, current.config
	go func() {
		defer s.finishCodexModelQualityWorker(account.ID)
		defer func() {
			releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = store.ReleaseCodexModelQuality(releaseCtx, account.ID, model, job.lease)
		}()
		s.runHistoricalQualityExtension(runCtx, job)
	}()
}

func (s *OpenAIGatewayService) runHistoricalQualityExtension(parent context.Context, job *codexModelQualityJob) {
	now := time.Now()
	expires := job.ticket.historicalExpires(job.config)
	budget := codexModelQualityBudget(now, expires, job.policy)
	until := codexModelQualityRouteDeadline(job.ticket, now.Add(budget))
	if expires.Add(-time.Second).Before(until) {
		until = expires.Add(-time.Second)
	}
	if !job.leaseExpiresAt.IsZero() && job.leaseExpiresAt.Add(-time.Second).Before(until) {
		until = job.leaseExpiresAt.Add(-time.Second)
	}
	if budget < 2*time.Second || until.Sub(now) < 2*time.Second {
		return
	}
	ctx, cancel := context.WithDeadline(parent, until)
	status := qualityStatusForJob(job)
	status.TicketExpiresAt = &expires
	status = s.evaluateCodexModelQuality(ctx, job, status)
	complete := parent.Err() == nil && ctx.Err() == nil
	cancel()
	if parent.Err() != nil {
		return
	}
	s.persistHistoricalQualityExtension(job, expires, complete && status.Status == "passed")
}

func (s *OpenAIGatewayService) persistHistoricalQualityExtension(job *codexModelQualityJob, expected time.Time, passed bool) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	account, err := s.accountRepo.GetByID(ctx, job.account.ID)
	if err != nil || account == nil {
		return false
	}
	key := openAICodexTicketKey(account.ID, job.ticket.Model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	inventory := s.codexTicketInventoryLocked(account, job.ticket.Model)
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	now := time.Now()
	selected := selectOpenAICodexTicket(inventory, account, cfg, now)
	if !sameCodexTicketLineage(selected, job.ticket) || !historicalQualityExtensionDue(selected, cfg, now) ||
		!selected.historicalExpires(cfg).Equal(expected) || job.scope != codexModelQualityScope(account, selected, cfg) {
		return false
	}
	replacement := cloneCodexTicketInventory(inventory)
	for _, slot := range codexTicketSlots(replacement) {
		if !sameCodexTicketLineage(slot, selected) {
			continue
		}
		slot.HistoricalQualityCheckedAt = now
		if passed {
			slot.HistoricalExtendedExpiresAt = expected.Add(time.Duration(cfg.HistoricalQualityExtendSeconds) * time.Second)
		}
		break
	}
	snapshot := cloneOpenAICodexTicketAccount(account)
	updated, err := s.persistOpenAICodexTicketResult(ctx, snapshot, job.ticket.Model, replacement)
	if !updated || err != nil {
		return false
	}
	s.openaiCodexTickets.Store(key, replacement)
	return true
}
