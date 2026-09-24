package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexModelQualitySchedulesWithoutWaitingForModelAndDeduplicates(t *testing.T) {
	s, r, job := qualityRuntimeFixture(t)
	r.lease = ""
	ctx, cancel := context.WithCancel(context.Background())
	s.codexModelQuality.ctx = ctx
	t.Cleanup(func() { cancel(); s.codexModelQuality.workers.Wait() })
	entered := make(chan struct{})
	s.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		close(entered)
		<-req.Context().Done()
		return nil, req.Context().Err()
	}}
	result := s.scheduleCodexModelQuality(ctx, job.account, job.ticket.Model, "manual", job.policy)
	require.True(t, result.Scheduled, result.Reason)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("background request did not start")
	}
	result = s.scheduleCodexModelQuality(ctx, job.account, job.ticket.Model, "manual", job.policy)
	require.False(t, result.Scheduled)
	require.Contains(t, []string{"capacity", "cooldown", "already_running"}, result.Reason)
	cancel()
	s.codexModelQuality.workers.Wait()
	require.Zero(t, s.codexModelQuality.active)
	require.Zero(t, r.writes)
}

func TestCodexModelQualityDisabledDoesNotSchedule(t *testing.T) {
	s, _, job := qualityRuntimeFixture(t)
	job.policy.Enabled = false
	result := s.scheduleCodexModelQuality(context.Background(), job.account, job.ticket.Model, "manual", job.policy)
	require.False(t, result.Scheduled)
	require.Equal(t, "disabled", result.Reason)
}

func TestCodexModelQualityLostLeaseDuringFinalFenceCannotRevoke(t *testing.T) {
	s, r, job := qualityRuntimeFixture(t)
	reads := 0
	r.getHook = func(context.Context) {
		reads++
		if reads == 2 {
			r.mu.Lock()
			r.lease = "another-instance"
			r.mu.Unlock()
		}
	}
	status := qualityStatusForJob(job)
	status.Status, status.Reason = "suspect", "model_mismatch"
	s.finishCodexModelQuality(job, r, status, time.Now())
	require.Equal(t, 3, reads)
	require.Zero(t, r.writes)
	require.Nil(t, r.record)
	require.False(t, s.codexTicketRevoked(openAICodexTicketKey(job.account.ID, job.ticket.Model), job.ticket))
}

func TestCodexModelQualityExpiredLeaseDoesNotPublishOrRevoke(t *testing.T) {
	s, r, job := qualityRuntimeFixture(t)
	job.leaseExpiresAt = time.Now().Add(-time.Second)
	status := qualityStatusForJob(job)
	status.Status, status.Reason = "suspect", "model_mismatch"
	s.finishCodexModelQuality(job, r, status, time.Now())
	require.Nil(t, r.record)
	require.Zero(t, r.writes)
}

func TestCodexModelQualityFingerprintSuspicionAndOptOutNeverRevoke(t *testing.T) {
	for _, reason := range []string{"fingerprint_mismatch", "model_mismatch"} {
		s, r, job := qualityRuntimeFixture(t)
		job.policy.QuarantineOnFailure = false
		_, err := s.settingService.UpdateCodexModelQualityPolicy(context.Background(), job.policy)
		require.NoError(t, err)
		job.policyHash = codexModelQualityPolicyHash(job.policy)
		status := qualityStatusForJob(job)
		status.Status, status.Reason = "suspect", reason
		s.finishCodexModelQuality(job, r, status, time.Now())
		require.Equal(t, "suspect", r.record.Status.Status)
		require.Zero(t, r.writes)
	}
}

func TestCodexModelQualityProtocolErrorNeverQuarantines(t *testing.T) {
	s, r, job := qualityRuntimeFixture(t)
	s.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("event: error\n" + qualityResponseBody("other", "{}")))}, nil
	}}
	s.executeCodexModelQuality(context.Background(), job, r)
	require.Equal(t, "inconclusive", r.record.Status.Status)
	require.Equal(t, "response_incomplete", r.record.Status.Reason)
	require.Zero(t, r.writes)
}

func TestCodexModelQualityCooldownAppliesToManualAndAnomaly(t *testing.T) {
	_, _, job := qualityRuntimeFixture(t)
	now := time.Now()
	next := now.Add(time.Hour)
	record := &CodexModelQualityRecord{Scope: job.scope, Policy: job.policyHash,
		Status: CodexModelQualityStatus{Status: "inconclusive", CheckedAt: &now, NextCheckAt: &next}}
	for _, source := range []string{"manual", "anomaly", "automatic"} {
		require.False(t, codexModelQualityDue(record, job, source, now))
	}
	require.False(t, codexModelQualityDue(record, job, "anomaly", now.Add(5*time.Minute)))
	require.False(t, codexModelQualityDue(record, job, "automatic", now.Add(5*time.Minute)))
	require.True(t, codexModelQualityDue(record, job, "anomaly", next))
}

func TestCodexModelQualityPassedTicketNeverNeedsRegularRecheck(t *testing.T) {
	_, _, job := qualityRuntimeFixture(t)
	now := time.Now()
	past := now.Add(-time.Hour)
	record := qualityAdmissionRecord(job, "passed", "capability_passed")
	record.Status.CheckedAt, record.Status.NextCheckAt = &past, &past
	for _, source := range []string{"automatic", "manual", "anomaly"} {
		require.False(t, codexModelQualityDue(record, job, source, now), source)
	}
	require.True(t, codexModelQualityDue(record, job, "diagnostic", now))
}

func TestCodexModelQualityNewTicketWaitsFromItsOwnCaptureTime(t *testing.T) {
	_, _, job := qualityRuntimeFixture(t)
	now := time.Now()
	job.ticket.CapturedAt = now
	ready := now.Add(time.Duration(job.policy.ReplacementCheckDelaySeconds) * time.Second)
	oldCapture, past := now.Add(-time.Hour), now.Add(-time.Minute)
	replaced := qualityAdmissionRecord(job, "quarantined", "capability_failed")
	replaced.Status.TicketCapturedAt, replaced.Status.NextCheckAt = &oldCapture, &past
	for _, previous := range []*CodexModelQualityRecord{nil, replaced} {
		for _, source := range []string{"automatic", "manual", "anomaly"} {
			require.False(t, codexModelQualityDue(previous, job, source, ready.Add(-time.Nanosecond)), source)
			require.True(t, codexModelQualityDue(previous, job, source, ready), source)
		}
		require.True(t, codexModelQualityDue(previous, job, "diagnostic", now))
	}
}

func TestCodexModelQualityCircuitPauseAppliesToEveryCheckSource(t *testing.T) {
	_, _, job := qualityRuntimeFixture(t)
	now, until := time.Now(), time.Now().Add(time.Hour)
	record := qualityAdmissionRecord(job, "inconclusive", "timeout")
	record.QualityPausedUntil = &until
	for _, source := range []string{"automatic", "manual", "anomaly", "diagnostic"} {
		require.False(t, codexModelQualityDue(record, job, source, now), source)
		require.True(t, codexModelQualityDue(record, job, source, until), source)
	}
}

func TestCodexModelQualitySchedulingReportsPauseBeforeTicketOrCapacity(t *testing.T) {
	for _, source := range []string{"automatic", "manual", "anomaly", "diagnostic"} {
		t.Run(source, func(t *testing.T) {
			s, repo, job := qualityRuntimeFixture(t)
			until := time.Now().Add(time.Hour)
			repo.record = qualityAdmissionRecord(job, "quarantined", "capability_failed")
			repo.record.QualityPausedUntil = &until
			s.openaiCodexTickets.Delete(openAICodexTicketKey(job.account.ID, job.ticket.Model))
			ctx := context.Background()
			if source == "diagnostic" {
				ctx = withCodexModelQualityDiagnostic(ctx)
			}
			result := s.scheduleCodexModelQuality(ctx, job.account, job.ticket.Model, source, job.policy)
			require.False(t, result.Scheduled)
			require.Equal(t, "quality_paused", result.Reason)
			require.Zero(t, s.codexModelQuality.active)
		})
	}
}

func TestCodexModelQualityStaleScopeRespectsRetryBackoff(t *testing.T) {
	_, _, job := qualityRuntimeFixture(t)
	now := time.Now()
	next := now.Add(time.Minute)
	record := &CodexModelQualityRecord{Scope: "old-scope", Policy: job.policyHash,
		Status: CodexModelQualityStatus{Status: "stale", Reason: "stale", NextCheckAt: &next}}
	require.False(t, codexModelQualityDue(record, job, "automatic", now))
	require.True(t, codexModelQualityDue(record, job, "automatic", next))
}
