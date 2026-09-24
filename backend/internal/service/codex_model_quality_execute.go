package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/modelquality"
	"go.uber.org/zap"
)

func (s *OpenAIGatewayService) executeCodexModelQuality(parent context.Context, job *codexModelQualityJob, store CodexModelQualityStore) {
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = store.ReleaseCodexModelQuality(ctx, job.account.ID, job.ticket.Model, job.lease)
	}()
	started := time.Now()
	status := qualityStatusForJob(job)
	budget := codexModelQualityBudget(started, *status.TicketExpiresAt, job.policy)
	until := started.Add(budget)
	if !job.leaseExpiresAt.IsZero() && job.leaseExpiresAt.Add(-time.Second).Before(until) {
		until = job.leaseExpiresAt.Add(-time.Second)
	}
	status.NextCheckAt = &until
	if budget == 0 {
		status.Status, status.Reason = "skipped", "insufficient_ttl"
	} else {
		// 初始化、保存运行状态和全部复验共同使用绝对期限，慢存储不能加回预算。
		ctx, cancel := context.WithDeadline(parent, until)
		if !saveCodexModelQuality(ctx, store, job, status) {
			cancel()
			return
		}
		stopWatch := s.watchCodexModelQuality(ctx, cancel, job)
		status = s.evaluateCodexModelQuality(ctx, job, status)
		if ctx.Err() != nil {
			status.Status, status.Reason = "inconclusive", "timeout"
		}
		cancel()
		<-stopWatch
	}
	s.finishCodexModelQuality(job, store, status, started)
}

func (s *OpenAIGatewayService) watchCodexModelQuality(ctx context.Context, cancel context.CancelFunc, job *codexModelQualityJob) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !s.codexModelQualityCurrent(ctx, job) {
					cancel()
					return
				}
			}
		}
	}()
	return done
}

func (s *OpenAIGatewayService) finishCodexModelQuality(job *codexModelQualityJob, store CodexModelQualityStore, status CodexModelQualityStatus, started time.Time) {
	deadline := time.Now().Add(5 * time.Second)
	if expires := job.ticket.effectiveExpiresAt(job.config); expires.Before(deadline) {
		deadline = expires
	}
	if !job.leaseExpiresAt.IsZero() && job.leaseExpiresAt.Add(-time.Second).Before(deadline) {
		deadline = job.leaseExpiresAt.Add(-time.Second)
	}
	base := context.Background()
	if job.diagnostic {
		base = withCodexModelQualityDiagnostic(base)
	}
	ctx, cancel := context.WithDeadline(base, deadline)
	defer cancel()
	now := time.Now()
	status.CheckedAt, status.DurationMS = &now, now.Sub(started).Milliseconds()
	if !s.codexModelQualityCurrent(ctx, job) {
		status.Status, status.Reason = "stale", "stale"
	}
	delay := job.policy.RetryIntervalSeconds
	if status.Status == "passed" || status.Reason == "capability_failed" || status.Reason == "model_mismatch" {
		delay = job.policy.ReplacementCheckDelaySeconds
	}
	next := now.Add(time.Duration(delay) * time.Second)
	status.NextCheckAt = &next
	if status.Status == "passed" {
		status.NextCheckAt = nil
	}
	quarantine := CodexModelQualityFailure(status) && (status.Reason == "capability_failed" || job.policy.QuarantineOnFailure) && !job.diagnostic
	if quarantine {
		s.quarantineCodexModelQuality(ctx, job, store, status)
	} else if !saveCodexModelQualityRecord(ctx, store, job, s.codexModelQualityRecord(ctx, store, job, status, now)) {
		return
	}
	r := &s.codexModelQuality
	r.mu.Lock()
	key := openAICodexTicketKey(job.account.ID, job.ticket.Model)
	if at, exists := r.anomalies[key]; exists && !at.After(started) {
		delete(r.anomalies, key)
	}
	r.mu.Unlock()
}

// 在票据锁内再次确认租约；持久化沿用条件更新，并保留检测任务的截止时间。
func (s *OpenAIGatewayService) quarantineCodexModelQuality(ctx context.Context, job *codexModelQualityJob, store CodexModelQualityStore, status CodexModelQualityStatus) {
	// 同实例保存策略与最终撤票串行；与正常发布保持一致的加锁顺序。
	s.settingService.codexTicketPublishMu.RLock()
	defer s.settingService.codexTicketPublishMu.RUnlock()
	if !s.codexModelQualityCurrent(ctx, job) {
		status.Status, status.Reason = "stale", "stale"
		saveCodexModelQuality(ctx, store, job, status)
		return
	}
	key := openAICodexTicketKey(job.account.ID, job.ticket.Model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	inventory := s.codexTicketInventoryLocked(job.account, job.ticket.Model)
	if ctx.Err() != nil || !codexTicketInventoryContains(inventory, job.ticket) || !s.codexModelQualityControlsCurrent(ctx, job) {
		status.Status, status.Reason = "stale", "stale"
		saveCodexModelQuality(ctx, store, job, status)
		return
	}
	// 先验证共享租约，只有票据条件更新成功后才发布已隔离状态。
	previous := codexModelQualityBaseRecord(ctx, store, job, status)
	if previous == nil {
		return
	}
	record := *previous
	advanceCodexModelQualityCircuit(&record, job, time.Now())
	if !saveCodexModelQualityRecord(ctx, store, job, &record) || ctx.Err() != nil {
		return
	}
	used := codexTicketWithInvalidation(job.ticket, newCodexTicketInvalidation("model_quality_"+status.Reason, "model_quality", nil))
	snapshot := cloneOpenAICodexTicketAccount(job.account)
	snapshot.Extra[openAICodexTicketExtraKey(used.Model)] = inventory
	tombstone := *codexTicketLeaf(used)
	tombstone.Revoked = true
	updated, err := s.persistOpenAICodexTicketResult(ctx, snapshot, used.Model, &tombstone)
	if !updated || err != nil || ctx.Err() != nil {
		status.Status, status.Reason = "suspect", "quarantine_persist_failed"
		if !updated && err == nil {
			status.Status, status.Reason = "stale", "stale"
		}
		previous.Status = status
		_ = saveCodexModelQualityRecord(ctx, store, job, previous)
		return
	}
	s.rememberCodexTicketRevocation(key, used)
	s.openaiCodexTickets.Store(key, revokeCodexTicketSlot(inventory, used))
	status.Status = "quarantined"
	record.Status = status
	_ = saveCodexModelQualityRecord(ctx, store, job, &record)
	s.markCodexQualityTicketFailed(ctx, job, used.Invalidation.Reason)
}

func (s *OpenAIGatewayService) markCodexQualityTicketFailed(ctx context.Context, job *codexModelQualityJob, reason string) {
	marker, ok := s.accountRepo.(codexTicketAttemptFailureMarker)
	if !ok || strings.TrimSpace(job.ticket.AttemptID) == "" {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := marker.MarkCodexTicketAttemptFailed(writeCtx, job.account.ID, job.ticket.AttemptID, job.ticket.Model, job.ticket.CapturedAt, reason); err != nil {
		logger.L().Warn("codex model quality history update failed", zap.Int64("account_id", job.account.ID))
	}
}

func (s *OpenAIGatewayService) evaluateCodexModelQuality(ctx context.Context, job *codexModelQualityJob, status CodexModelQualityStatus) CodexModelQualityStatus {
	score, err := s.runCodexQualityCapability(ctx, job, &status)
	if err != nil {
		return codexQualityIncomplete(status, err)
	}
	status.CapabilityScore = &score
	if status.ModelIdentity == "mismatch" {
		status.Status, status.Reason = "suspect", "model_mismatch"
		return status
	}
	if score < 75 {
		second, err := s.runCodexQualityCapability(ctx, job, &status)
		if err != nil {
			return codexQualityIncomplete(status, err)
		}
		average := (score + second) / 2
		status.CapabilityScore = &average
		if second < 75 {
			status.Status, status.Reason = "suspect", "capability_failed"
			return status
		}
	}
	if status.ModelIdentity == "mismatch" {
		status.Status, status.Reason = "suspect", "model_mismatch"
		return status
	}
	status.Status, status.Reason = "passed", "capability_passed"
	if !job.policy.FingerprintEnabled {
		return status
	}
	if !modelquality.SupportsModel(job.ticket.Model) {
		status.Status, status.Reason = "inconclusive", "unsupported_model"
		return status
	}
	return s.evaluateCodexQualityFingerprint(ctx, job, status)
}

func (s *OpenAIGatewayService) runCodexQualityCapability(ctx context.Context, job *codexModelQualityJob, status *CodexModelQualityStatus) (float64, error) {
	challenge := newCodexQualityCapabilityChallenge()
	return s.answerCodexQualityCapability(ctx, job, status, challenge)
}

func (s *OpenAIGatewayService) answerCodexQualityCapability(ctx context.Context, job *codexModelQualityJob, status *CodexModelQualityStatus, challenge codexQualityChallenge) (float64, error) {
	output, err := s.requestCodexModelQuality(ctx, job, challenge.Prompt)
	status.Requests++
	if err != nil {
		return 0, err
	}
	mergeCodexQualityIdentity(status, output.identity)
	if output.identity == "mismatch" {
		return 0, nil
	}
	score, err := scoreCodexQualityCapability(challenge, output.text)
	if err != nil {
		return 0, errors.New("invalid_capability_output")
	}
	return score, nil
}

func (s *OpenAIGatewayService) evaluateCodexQualityFingerprint(ctx context.Context, job *codexModelQualityJob, status CodexModelQualityStatus) CodexModelQualityStatus {
	var samples []modelquality.Sample
	for round := 0; round < 3; round++ {
		prompt, count := modelquality.GenerateFingerprintChallenge()
		output, err := s.requestCodexModelQuality(ctx, job, prompt)
		status.Requests++
		if err != nil {
			return codexQualityIncomplete(status, err)
		}
		mergeCodexQualityIdentity(&status, output.identity)
		if status.ModelIdentity == "mismatch" {
			status.Status, status.Reason = "suspect", "model_mismatch"
			return status
		}
		samples = append(samples, modelquality.Sample{Text: output.text, ExpectedCount: count})
		result, err := modelquality.Analyze(samples)
		if err != nil || result.UsedOutputs != len(samples) {
			return codexQualityIncomplete(status, errors.New("invalid_fingerprint_output"))
		}
		status.SampleCount = result.UsedOutputs
		status.FingerprintCandidate, status.FingerprintProbability = result.Candidate, &result.Probability
		status.FingerprintSimilarity = &result.Similarity
		// 这些阈值仅决定是否继续采样，不能把闭集概率转为身份认证。
		if result.Candidate == job.ticket.Model && result.Probability >= .9 && result.Margin >= .2 && result.Similarity >= .5 {
			status.Status, status.Reason = "passed", "fingerprint_match"
			return status
		}
		status.Status, status.Reason = "inconclusive", "fingerprint_uncertain"
		if result.Candidate != job.ticket.Model && result.Probability >= .9 && result.Similarity >= .5 {
			status.Status, status.Reason = "suspect", "fingerprint_mismatch"
		}
	}
	return status
}

func mergeCodexQualityIdentity(status *CodexModelQualityStatus, identity string) {
	if identity == "mismatch" || status.ModelIdentity != "mismatch" && identity == "match" {
		status.ModelIdentity = identity
	}
}

func codexQualityIncomplete(status CodexModelQualityStatus, err error) CodexModelQualityStatus {
	status.Status, status.Reason = "inconclusive", err.Error()
	return status
}
