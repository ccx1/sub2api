package service

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type codexModelQualityRuntime struct {
	mu              sync.Mutex
	ctx             context.Context
	active          int
	activeByAccount map[int64]int
	workers         sync.WaitGroup
	anomalies       map[string]time.Time
}

func (s *OpenAIGatewayService) runCodexModelQualityLoop(ctx context.Context) {
	r := &s.codexModelQuality
	r.mu.Lock()
	r.ctx = ctx
	if r.anomalies == nil {
		r.anomalies = make(map[string]time.Time)
	}
	if r.activeByAccount == nil {
		r.activeByAccount = make(map[int64]int)
	}
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		r.ctx = nil
		r.mu.Unlock()
		r.workers.Wait()
	}()
	timer := time.NewTicker(15 * time.Second)
	defer timer.Stop()
	for {
		s.scanCodexModelQuality(ctx)
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}

func (s *OpenAIGatewayService) scanCodexModelQuality(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p, err := s.settingService.GetCodexModelQualityPolicy(ctx)
	if err != nil || !p.Enabled || s.accountRepo == nil || !s.openAICodexTicketEnabledContext(ctx) {
		return
	}
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return
	}
	models := prioritizedCodexModelQualityModels(s.openAICodexTicketConfigContext(ctx).Models, p.ModelPriorities)
	// Model priority is the primary queue order. Accounts are already returned
	// in their normal scheduler priority order, so that order remains the tie
	// breaker within one model.
	for _, model := range models {
		for i := range accounts {
			if ctx.Err() != nil {
				return
			}
			result := s.scheduleCodexModelQuality(ctx, &accounts[i], model, "automatic", p)
			switch result.Reason {
			case "capacity":
				return
			case "account_capacity":
				continue
			}
		}
	}
}

func prioritizedCodexModelQualityModels(models []string, priorities map[string]int) []string {
	type candidate struct {
		model    string
		priority int
		index    int
	}
	seen := make(map[string]struct{}, len(models))
	candidates := make([]candidate, 0, len(models))
	for index, model := range models {
		model = normalizeOpenAICodexTicketModel(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		candidates = append(candidates, candidate{model: model, priority: priorities[model], index: index})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].priority != candidates[j].priority {
			return candidates[i].priority > candidates[j].priority
		}
		return candidates[i].index < candidates[j].index
	})
	out := make([]string, 0, len(candidates))
	for _, item := range candidates {
		out = append(out, item.model)
	}
	return out
}

func (s *OpenAIGatewayService) markCodexModelQualityAnomaly(accountID int64, model string) {
	if s == nil {
		return
	}
	r := &s.codexModelQuality
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.ctx == nil || r.ctx.Err() != nil {
		return
	}
	key := openAICodexTicketKey(accountID, model)
	if _, exists := r.anomalies[key]; !exists && len(r.anomalies) < 4096 {
		r.anomalies[key] = time.Now()
	}
}

func (s *OpenAIGatewayService) takeCodexModelQualityWorker(accountID int64, limit, accountLimit int) (context.Context, bool, string) {
	return s.takeCodexModelQualityWorkerWithContext(accountID, limit, accountLimit, false)
}

// Diagnostic checks are explicit operator actions and must remain available
// when the background harvester has not been started. Automatic checks still
// require the harvester context, while a diagnostic gets an independent
// background context whose execution is bounded by the ticket budget.
func (s *OpenAIGatewayService) takeCodexModelQualityDiagnosticWorker(accountID int64, limit, accountLimit int) (context.Context, bool, string) {
	return s.takeCodexModelQualityWorkerWithContext(accountID, limit, accountLimit, true)
}

func (s *OpenAIGatewayService) takeCodexModelQualityWorkerWithContext(accountID int64, limit, accountLimit int, standalone bool) (context.Context, bool, string) {
	r := &s.codexModelQuality
	r.mu.Lock()
	defer r.mu.Unlock()
	runCtx := r.ctx
	if (runCtx == nil || runCtx.Err() != nil) && standalone {
		runCtx = context.Background()
	}
	if runCtx == nil || runCtx.Err() != nil || r.active >= limit {
		return nil, false, "capacity"
	}
	if r.activeByAccount == nil {
		r.activeByAccount = make(map[int64]int)
	}
	if r.activeByAccount[accountID] >= accountLimit {
		return nil, false, "account_capacity"
	}
	r.active++
	r.activeByAccount[accountID]++
	r.workers.Add(1)
	return runCtx, true, ""
}

func (s *OpenAIGatewayService) finishCodexModelQualityWorker(accountID int64) {
	r := &s.codexModelQuality
	r.mu.Lock()
	if r.active > 0 {
		r.active--
	}
	if count := r.activeByAccount[accountID]; count > 1 {
		r.activeByAccount[accountID] = count - 1
	} else {
		delete(r.activeByAccount, accountID)
	}
	r.mu.Unlock()
	r.workers.Done()
}

func (s *OpenAIGatewayService) scheduleCodexModelQuality(ctx context.Context, account *Account, model, source string, p CodexModelQualityPolicy) CodexModelQualityScheduleResult {
	return s.scheduleCodexModelQualityWithPolicyHash(ctx, account, model, source, p, "")
}

// scheduleCodexModelQualityWithPolicyHash lets an explicit diagnostic use a
// temporary execution policy (for example, enabled without quarantine) while
// retaining the persisted policy hash used by the current-ticket fence.
func (s *OpenAIGatewayService) scheduleCodexModelQualityWithPolicyHash(ctx context.Context, account *Account, model, source string, p CodexModelQualityPolicy, policyHash string) CodexModelQualityScheduleResult {
	if s.codexModelQualityCircuitPaused(ctx, account, model) {
		return CodexModelQualityScheduleResult{Reason: "quality_paused"}
	}
	job, reason := s.prepareCodexModelQuality(ctx, account, model, p)
	if job == nil {
		return CodexModelQualityScheduleResult{Reason: reason}
	}
	if strings.TrimSpace(policyHash) != "" {
		job.policyHash = policyHash
	}
	store, ok := s.accountRepo.(CodexModelQualityStore)
	if !ok {
		return CodexModelQualityScheduleResult{Reason: "shared_state_unavailable"}
	}
	previous, err := store.LoadCodexModelQuality(ctx, account.ID, model)
	if err != nil {
		return CodexModelQualityScheduleResult{Reason: "shared_state_unavailable"}
	}
	source = s.codexModelQualitySource(account.ID, model, source)
	if !codexModelQualityDue(previous, job, source, time.Now()) {
		return CodexModelQualityScheduleResult{Reason: "cooldown"}
	}
	var runCtx context.Context
	var acquired bool
	var capacityReason string
	if job.diagnostic {
		runCtx, acquired, capacityReason = s.takeCodexModelQualityDiagnosticWorker(account.ID, p.Concurrency, p.AccountConcurrency)
	} else {
		runCtx, acquired, capacityReason = s.takeCodexModelQualityWorker(account.ID, p.Concurrency, p.AccountConcurrency)
	}
	if !acquired {
		return CodexModelQualityScheduleResult{Reason: capacityReason}
	}
	job.lease, job.source = uuid.NewString(), source
	leaseTTL := time.Duration(p.TimeoutSeconds+15) * time.Second
	job.leaseExpiresAt = time.Now().Add(leaseTTL)
	locked, err := store.AcquireCodexModelQuality(ctx, account.ID, model, job.lease, leaseTTL)
	if err != nil || !locked {
		s.finishCodexModelQualityWorker(account.ID)
		if err != nil {
			return CodexModelQualityScheduleResult{Reason: "shared_state_unavailable"}
		}
		return CodexModelQualityScheduleResult{Reason: "already_running"}
	}
	// 获锁后重读，关闭其它实例刚完成检测与本实例抢锁之间的窗口。
	previous, err = store.LoadCodexModelQuality(ctx, account.ID, model)
	if err != nil || !codexModelQualityDue(previous, job, source, time.Now()) {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = store.ReleaseCodexModelQuality(releaseCtx, account.ID, model, job.lease)
		cancel()
		s.finishCodexModelQualityWorker(account.ID)
		return CodexModelQualityScheduleResult{Reason: "cooldown"}
	}
	go func() {
		defer s.finishCodexModelQualityWorker(account.ID)
		if job.diagnostic {
			// The worker runtime context is shared by automatic checks. Preserve
			// the diagnostic marker so current-ticket fences continue to allow a
			// manual check even when the persisted background policy is disabled.
			runCtx = withCodexModelQualityDiagnostic(runCtx)
		}
		s.executeCodexModelQuality(runCtx, job, store)
	}()
	return CodexModelQualityScheduleResult{Scheduled: true, Reason: "scheduled"}
}

func (s *OpenAIGatewayService) codexModelQualitySource(accountID int64, model, source string) string {
	if source == "diagnostic" {
		// An operator-triggered run must remain distinguishable from an
		// anomaly-triggered run even when the same account/model is marked for
		// anomaly follow-up.
		return source
	}
	r := &s.codexModelQuality
	r.mu.Lock()
	defer r.mu.Unlock()
	key := openAICodexTicketKey(accountID, model)
	if at, exists := r.anomalies[key]; exists {
		if time.Since(at) < time.Hour {
			return "anomaly"
		}
		delete(r.anomalies, key)
	}
	return source
}

func codexModelQualityDue(previous *CodexModelQualityRecord, job *codexModelQualityJob, source string, now time.Time) bool {
	if previous != nil && previous.QualityPausedUntil != nil && now.Before(*previous.QualityPausedUntil) {
		return false
	}
	if source == "diagnostic" {
		// An explicit administrator action is a force recheck. The shared lease
		// below still prevents two workers from probing the same ticket at once.
		return true
	}
	if previous != nil && previous.Status.Status == "passed" && previous.Status.TicketCapturedAt != nil &&
		previous.Status.TicketCapturedAt.Equal(job.ticket.CapturedAt) {
		// A ticket that already passed quality is valid for its whole lifetime;
		// do not spend another upstream request on periodic rechecks.
		return false
	}
	if now.Before(job.ticket.CapturedAt.Add(time.Duration(job.policy.ReplacementCheckDelaySeconds) * time.Second)) {
		return false
	}
	if previous == nil {
		return true
	}
	if previous.Status.Status != "passed" && previous.Status.NextCheckAt != nil && now.Before(*previous.Status.NextCheckAt) {
		return false
	}
	if previous.Status.TicketCapturedAt != nil && !previous.Status.TicketCapturedAt.Equal(job.ticket.CapturedAt) {
		return true
	}
	if previous.Scope != job.scope || previous.Policy != job.policyHash {
		// A changed ticket, proxy or policy needs one fresh check immediately.
		// Once that check has recorded a stale result, keep its retry backoff;
		// otherwise a scope that changes on every harvest can bypass the retry
		// interval forever and start a worker on every scan tick.
		if previous.Status.NextCheckAt != nil && now.Before(*previous.Status.NextCheckAt) {
			return false
		}
		return true
	}
	status := previous.Status
	if status.Status == "running" {
		return status.NextCheckAt == nil || !now.Before(*status.NextCheckAt)
	}
	if source != "automatic" && status.CheckedAt != nil {
		return !now.Before(status.CheckedAt.Add(time.Duration(job.policy.RetryIntervalSeconds) * time.Second))
	}
	return status.NextCheckAt == nil || !now.Before(*status.NextCheckAt)
}
