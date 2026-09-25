package service

import (
	"context"
	"time"
)

// 调用方持有账号、模型共享租约；读取失败不能覆盖已有连续失败计数。
func codexModelQualityBaseRecord(ctx context.Context, store CodexModelQualityStore, job *codexModelQualityJob, status CodexModelQualityStatus) *CodexModelQualityRecord {
	previous, err := store.LoadCodexModelQuality(ctx, job.account.ID, job.ticket.Model)
	if err != nil {
		return nil
	}
	record := &CodexModelQualityRecord{}
	if previous != nil {
		*record = *previous
	}
	record.Status, record.Scope, record.Policy = status, job.scope, job.policyHash
	return record
}

func saveCodexModelQuality(ctx context.Context, store CodexModelQualityStore, job *codexModelQualityJob, status CodexModelQualityStatus) bool {
	return saveCodexModelQualityRecord(ctx, store, job, codexModelQualityBaseRecord(ctx, store, job, status))
}

func saveCodexModelQualityRecord(ctx context.Context, store CodexModelQualityStore, job *codexModelQualityJob, record *CodexModelQualityRecord) bool {
	if ctx.Err() != nil || record == nil {
		return false
	}
	record.Status.ConsecutiveLowQuality = record.ConsecutiveLowQuality
	record.Status.QualityPausedUntil = record.QualityPausedUntil
	// 成功结论至少保留到该票过期，最长冷却也不能被记录过期提前解除。
	ttl := 24 * time.Hour
	if remaining := time.Until(job.ticket.effectiveExpiresAt(job.config)) + time.Hour; remaining > ttl {
		ttl = remaining
	}
	if record.QualityPausedUntil != nil && time.Until(*record.QualityPausedUntil)+time.Hour > ttl {
		ttl = time.Until(*record.QualityPausedUntil) + time.Hour
	}
	ok, err := store.SaveCodexModelQuality(ctx, job.account.ID, job.ticket.Model, job.lease, record, ttl)
	return err == nil && ok
}

func (s *OpenAIGatewayService) codexModelQualityRecord(ctx context.Context, store CodexModelQualityStore, job *codexModelQualityJob, status CodexModelQualityStatus, now time.Time) *CodexModelQualityRecord {
	record := codexModelQualityBaseRecord(ctx, store, job, status)
	if record == nil || job.diagnostic {
		return record
	}
	advanceCodexModelQualityCircuit(record, job, now)
	return record
}

func advanceCodexModelQualityCircuit(record *CodexModelQualityRecord, job *codexModelQualityJob, now time.Time) {
	if record.QualityPausedUntil != nil && !now.Before(*record.QualityPausedUntil) {
		record.ConsecutiveLowQuality, record.QualityPausedUntil = 0, nil
	}
	status := record.Status
	if status.Status == "passed" {
		record.ConsecutiveLowQuality, record.QualityPausedUntil, record.LastLowQualityTicket = 0, nil, ""
		return
	}
	if !codexQualityAnswerFailure(status.Reason) || !CodexModelQualityFailure(status) {
		return
	}
	identity := codexModelQualityHash([]string{job.ticket.Model, job.ticket.lineageCapturedAt().UTC().Format(time.RFC3339Nano), openAICodexTicketAccountBinding(job.account), job.ticket.SessionID, job.ticket.Egress})
	if record.LastLowQualityTicket == identity {
		return
	}
	record.LastLowQualityTicket = identity
	record.ConsecutiveLowQuality++
	if record.ConsecutiveLowQuality >= job.policy.LowQualityConsecutiveThreshold && job.policy.LowQualityConsecutiveThreshold > 0 {
		pausedUntil := now.Add(time.Duration(job.policy.LowQualityCooldownSeconds) * time.Second)
		record.QualityPausedUntil = &pausedUntil
	}
}
