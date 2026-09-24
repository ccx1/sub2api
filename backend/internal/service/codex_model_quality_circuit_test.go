package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexModelQualityDistinctFailuresAccumulateAcrossRunningSaves(t *testing.T) {
	s, repo, job := qualityRuntimeFixture(t)
	count := 0
	s.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		record, err := repo.LoadCodexModelQuality(context.Background(), job.account.ID, job.ticket.Model)
		require.NoError(t, err)
		require.Equal(t, "running", record.Status.Status)
		require.Equal(t, count-1, record.ConsecutiveLowQuality)
		require.Equal(t, count-1, record.Status.ConsecutiveLowQuality)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(qualityResponseBody(job.ticket.Model,
			`{"selected":[],"transformed":[],"checksum":-999,"flags":[],"final":-999}`)))}, nil
	}}
	for count = 1; count <= job.policy.LowQualityConsecutiveThreshold; count++ {
		if count > 1 {
			job = qualityCircuitReplacementJob(t, s, repo, job)
		}
		s.executeCodexModelQuality(context.Background(), job, repo)
		require.Equal(t, "quarantined", repo.record.Status.Status)
		require.Equal(t, "capability_failed", repo.record.Status.Reason)
		require.Equal(t, count, repo.record.ConsecutiveLowQuality)
		require.Equal(t, count, repo.record.Status.ConsecutiveLowQuality)
		require.NotEmpty(t, repo.record.LastLowQualityTicket)
		if count < job.policy.LowQualityConsecutiveThreshold {
			require.Nil(t, repo.record.QualityPausedUntil)
			require.False(t, s.codexModelQualityCircuitPaused(context.Background(), repo.account, job.ticket.Model))
		} else {
			require.NotNil(t, repo.record.QualityPausedUntil)
			require.WithinDuration(t, time.Now().Add(time.Duration(job.policy.LowQualityCooldownSeconds)*time.Second), *repo.record.QualityPausedUntil, time.Second)
			require.True(t, s.codexModelQualityCircuitPaused(context.Background(), repo.account, job.ticket.Model))
		}
	}
	job = qualityCircuitReplacementJob(t, s, repo, job)
	expired := time.Now().Add(-time.Second)
	repo.record.QualityPausedUntil = &expired
	status := qualityStatusForJob(job)
	status.Status, status.Reason = "suspect", "capability_failed"
	s.finishCodexModelQuality(job, repo, status, time.Now())
	require.Equal(t, 1, repo.record.ConsecutiveLowQuality)
	require.Nil(t, repo.record.QualityPausedUntil)
}

func qualityCircuitReplacementJob(t *testing.T, s *OpenAIGatewayService, repo *qualityRuntimeRepo, previous *codexModelQualityJob) *codexModelQualityJob {
	t.Helper()
	replacement := codexTicketLeaf(previous.ticket)
	replacement.CapturedAt = replacement.CapturedAt.Add(time.Second)
	s.openaiCodexTickets.Store(openAICodexTicketKey(repo.account.ID, replacement.Model), replacement)
	job, reason := s.prepareCodexModelQuality(context.Background(), repo.account, replacement.Model, previous.policy)
	require.NotNil(t, job, reason)
	job.lease, repo.lease, job.source = "quality-lease", "quality-lease", "automatic"
	job.leaseExpiresAt = time.Now().Add(time.Minute)
	return job
}

func TestCodexModelQualityQuarantineRetryCountsOnlySuccessfullyRevokedTicket(t *testing.T) {
	s, repo, job := qualityRuntimeFixture(t)
	repo.casHook = func(context.Context) (bool, error) { return false, errors.New("persist unavailable") }
	status := qualityStatusForJob(job)
	status.Status, status.Reason = "suspect", "capability_failed"
	for range 2 {
		s.finishCodexModelQuality(job, repo, status, time.Now())
		require.Equal(t, "quarantine_persist_failed", repo.record.Status.Reason)
		require.LessOrEqual(t, repo.record.ConsecutiveLowQuality, 1)
		require.Nil(t, repo.record.QualityPausedUntil)
	}
	require.Equal(t, 2, repo.writes)
	repo.casHook = nil
	s.finishCodexModelQuality(job, repo, status, time.Now())
	require.Equal(t, "quarantined", repo.record.Status.Status)
	require.Equal(t, 1, repo.record.ConsecutiveLowQuality)
	replayed := s.codexModelQualityRecord(context.Background(), repo, job, status, time.Now())
	require.Equal(t, 1, replayed.ConsecutiveLowQuality, "the same ticket cannot advance the streak twice")
}

func TestCodexModelQualityPassedTicketResetsFailureStreak(t *testing.T) {
	s, repo, job := qualityRuntimeFixture(t)
	repo.record = qualityAdmissionRecord(job, "suspect", "capability_failed")
	repo.record.ConsecutiveLowQuality = 2
	repo.record.LastLowQualityTicket = "previous-ticket"
	status := qualityStatusForJob(job)
	status.Status, status.Reason = "passed", "capability_passed"
	s.finishCodexModelQuality(job, repo, status, time.Now())
	require.Equal(t, "passed", repo.record.Status.Status)
	require.Zero(t, repo.record.ConsecutiveLowQuality)
	require.Zero(t, repo.record.Status.ConsecutiveLowQuality)
	require.Empty(t, repo.record.LastLowQualityTicket)
	require.Nil(t, repo.record.QualityPausedUntil)
}

func TestCodexModelQualityUnconfirmedResultsDoNotCountAsLowQuality(t *testing.T) {
	for _, tc := range []struct{ status, reason string }{
		{"stale", "stale"}, {"inconclusive", "timeout"}, {"suspect", "fingerprint_mismatch"},
		{"suspect", "model_mismatch"}, {"skipped", "insufficient_ttl"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			s, repo, job := qualityRuntimeFixture(t)
			repo.record = qualityAdmissionRecord(job, "suspect", "capability_failed")
			repo.record.ConsecutiveLowQuality, repo.record.LastLowQualityTicket = 2, "previous-ticket"
			status := qualityStatusForJob(job)
			status.Status, status.Reason = tc.status, tc.reason
			s.finishCodexModelQuality(job, repo, status, time.Now())
			require.Equal(t, 2, repo.record.ConsecutiveLowQuality)
			require.Equal(t, "previous-ticket", repo.record.LastLowQualityTicket)
			require.Nil(t, repo.record.QualityPausedUntil)
		})
	}
}

func TestCodexModelQualityRunningSavePreservesCircuitState(t *testing.T) {
	_, repo, job := qualityRuntimeFixture(t)
	until := time.Now().Add(time.Hour)
	repo.record = qualityAdmissionRecord(job, "quarantined", "capability_failed")
	repo.record.ConsecutiveLowQuality, repo.record.LastLowQualityTicket = 3, "previous-ticket"
	repo.record.QualityPausedUntil = &until
	require.True(t, saveCodexModelQuality(context.Background(), repo, job, qualityStatusForJob(job)))
	require.Equal(t, "running", repo.record.Status.Status)
	require.Equal(t, 3, repo.record.ConsecutiveLowQuality)
	require.Equal(t, 3, repo.record.Status.ConsecutiveLowQuality)
	require.Equal(t, "previous-ticket", repo.record.LastLowQualityTicket)
	require.Equal(t, &until, repo.record.QualityPausedUntil)
	require.Equal(t, &until, repo.record.Status.QualityPausedUntil)
}

func TestCodexModelQualityCapabilityFailureRevokesDespiteQuarantineOptOut(t *testing.T) {
	s, repo, job := qualityRuntimeFixture(t)
	job.policy.QuarantineOnFailure = false
	_, err := s.settingService.UpdateCodexModelQualityPolicy(context.Background(), job.policy)
	require.NoError(t, err)
	job.policyHash = codexModelQualityPolicyHash(job.policy)
	status := qualityStatusForJob(job)
	status.Status, status.Reason = "suspect", "capability_failed"
	s.finishCodexModelQuality(job, repo, status, time.Now())
	require.Equal(t, "quarantined", repo.record.Status.Status)
	require.Equal(t, 1, repo.record.ConsecutiveLowQuality)
	require.Equal(t, 1, repo.writes)
	require.True(t, s.codexTicketRevoked(openAICodexTicketKey(job.account.ID, job.ticket.Model), job.ticket))
}

func TestCodexModelQualityDiagnosticDoesNotChangeAutomaticFailureStreak(t *testing.T) {
	for _, tc := range []struct{ status, reason string }{
		{"passed", "capability_passed"}, {"suspect", "capability_failed"},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			s, repo, job := qualityRuntimeFixture(t)
			job.diagnostic = true
			expired := time.Now().Add(-time.Second)
			repo.record = qualityAdmissionRecord(job, "quarantined", "capability_failed")
			repo.record.ConsecutiveLowQuality, repo.record.LastLowQualityTicket = 3, "previous-ticket"
			repo.record.QualityPausedUntil = &expired
			status := qualityStatusForJob(job)
			status.Status, status.Reason = tc.status, tc.reason
			s.finishCodexModelQuality(job, repo, status, time.Now())
			require.Equal(t, tc.status, repo.record.Status.Status)
			require.Equal(t, 3, repo.record.ConsecutiveLowQuality)
			require.Equal(t, "previous-ticket", repo.record.LastLowQualityTicket)
			require.Equal(t, &expired, repo.record.QualityPausedUntil)
			require.Zero(t, repo.writes)
		})
	}
}

func TestCodexModelQualityCircuitPausesAutomaticAndManualHarvest(t *testing.T) {
	for _, manual := range []bool{false, true} {
		s, repo, job := qualityRuntimeFixture(t)
		until := time.Now().Add(time.Hour)
		repo.record = qualityAdmissionRecord(job, "quarantined", "capability_failed")
		repo.record.QualityPausedUntil = &until
		s.openaiCodexTickets.Delete(openAICodexTicketKey(job.account.ID, job.ticket.Model))
		calls := 0
		s.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
			calls++
			return nil, errors.New("unexpected harvest")
		}}
		ctx := context.Background()
		if manual {
			ctx = withCodexTicketManualRetry(ctx)
		}
		require.True(t, s.codexModelQualityCircuitPaused(ctx, repo.account, job.ticket.Model))
		s.probeOnceOpenAICodexTicket(ctx, repo.account, job.ticket.Model)
		_, queued := s.openaiCodexTicketQueues.Load(job.account.ID)
		require.False(t, queued, "manual=%v must stop before queue admission", manual)
		s.harvestVerifiedOpenAICodexTicket(ctx, repo.account, job.ticket.Model)
		require.Zero(t, calls, "manual=%v", manual)
		require.Zero(t, repo.writes)
	}
}
