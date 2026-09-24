package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func qualityAdmissionRecord(job *codexModelQualityJob, status, reason string) *CodexModelQualityRecord {
	result := qualityStatusForJob(job)
	result.Status, result.Reason = status, reason
	return &CodexModelQualityRecord{Status: result, Scope: job.scope, Policy: job.policyHash}
}

func TestCodexModelQualityAdmissionBlocksConfirmedFailure(t *testing.T) {
	s, repo, job := qualityRuntimeFixture(t)
	repo.record = qualityAdmissionRecord(job, "suspect", "capability_failed")

	require.True(t, s.codexModelQualityPaused(context.Background(), repo.account, job.ticket.Model))
	require.True(t, s.openAICodexTicketBlocksAccountContext(context.Background(), repo.account, job.ticket.Model))
	_, err := s.applyOpenAICodexTicketSnapshot(context.Background(), repo.account, job.ticket.Model, http.Header{})
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
}

func TestCodexModelQualityAdmissionLeavesPendingResultSchedulable(t *testing.T) {
	s, repo, job := qualityRuntimeFixture(t)
	repo.record = qualityAdmissionRecord(job, "pending", "not_checked")

	require.False(t, s.codexModelQualityPaused(context.Background(), repo.account, job.ticket.Model))
	require.False(t, s.openAICodexTicketBlocksAccountContext(context.Background(), repo.account, job.ticket.Model))
	_, err := s.applyOpenAICodexTicketSnapshot(context.Background(), repo.account, job.ticket.Model, http.Header{})
	require.NoError(t, err)
}

func TestCodexModelQualityAdmissionLeavesFingerprintMismatchSchedulable(t *testing.T) {
	s, repo, job := qualityRuntimeFixture(t)
	repo.record = qualityAdmissionRecord(job, "suspect", "fingerprint_mismatch")

	require.False(t, s.codexModelQualityPaused(context.Background(), repo.account, job.ticket.Model))
	require.False(t, s.openAICodexTicketBlocksAccountContext(context.Background(), repo.account, job.ticket.Model))
	_, err := s.applyOpenAICodexTicketSnapshot(context.Background(), repo.account, job.ticket.Model, http.Header{})
	require.NoError(t, err)
}

func TestCodexModelQualityAdmissionKeepsPauseWhenRevocationSettingChanges(t *testing.T) {
	s, repo, job := qualityRuntimeFixture(t)
	repo.record = qualityAdmissionRecord(job, "suspect", "capability_failed")

	policy := job.policy
	policy.QuarantineOnFailure = false
	_, err := s.settingService.UpdateCodexModelQualityPolicy(context.Background(), policy)
	require.NoError(t, err)
	require.True(t, s.codexModelQualityPaused(context.Background(), repo.account, job.ticket.Model))
}

func TestCodexModelQualityAdmissionBlocksFailedQuarantinePersistence(t *testing.T) {
	s, repo, job := qualityRuntimeFixture(t)
	repo.record = qualityAdmissionRecord(job, "suspect", "quarantine_persist_failed")

	require.True(t, s.codexModelQualityPaused(context.Background(), repo.account, job.ticket.Model))
}

func TestCodexModelQualityAdmissionReleasesReplacementTicket(t *testing.T) {
	s, repo, job := qualityRuntimeFixture(t)
	repo.record = qualityAdmissionRecord(job, "quarantined", "capability_failed")
	replacement := codexTicketLeaf(job.ticket)
	replacement.CapturedAt = time.Now().Add(time.Second)
	s.openaiCodexTickets.Store(openAICodexTicketKey(job.account.ID, job.ticket.Model), replacement)

	require.False(t, s.codexModelQualityPaused(context.Background(), repo.account, job.ticket.Model))
	require.False(t, s.openAICodexTicketBlocksAccountContext(context.Background(), repo.account, job.ticket.Model))
	_, err := s.applyOpenAICodexTicketSnapshot(context.Background(), repo.account, job.ticket.Model, http.Header{})
	require.NoError(t, err)
}

func TestCodexModelQualityCircuitPauseLeavesReplacementAvailableForTraffic(t *testing.T) {
	s, repo, job := qualityRuntimeFixture(t)
	until := time.Now().Add(time.Hour)
	repo.record = qualityAdmissionRecord(job, "quarantined", "capability_failed")
	repo.record.QualityPausedUntil = &until
	repo.record.ConsecutiveLowQuality = job.policy.LowQualityConsecutiveThreshold
	replacement := codexTicketLeaf(job.ticket)
	replacement.CapturedAt = time.Now()
	s.openaiCodexTickets.Store(openAICodexTicketKey(job.account.ID, job.ticket.Model), replacement)

	require.True(t, s.codexModelQualityCircuitPaused(context.Background(), repo.account, job.ticket.Model))
	require.False(t, s.codexModelQualityPaused(context.Background(), repo.account, job.ticket.Model))
	require.False(t, s.openAICodexTicketBlocksAccountContext(context.Background(), repo.account, job.ticket.Model))
	_, err := s.applyOpenAICodexTicketSnapshot(context.Background(), repo.account, job.ticket.Model, http.Header{})
	require.NoError(t, err)
}

func TestPrioritizedCodexModelQualityModelsUsesStableConfiguredOrder(t *testing.T) {
	got := prioritizedCodexModelQualityModels([]string{"low", "high", "same", "high", "none"}, map[string]int{"high": 100, "same": 50, "low": 1})
	require.Equal(t, []string{"high", "same", "low", "none"}, got)
}
