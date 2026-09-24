package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestCodexModelQualityDiagnosticModelsUsesConfiguredOrderAndDeduplicates(t *testing.T) {
	got := codexModelQualityDiagnosticModels(
		[]string{"gpt-6-astra", "gpt-5.6-sol"},
		[]string{" gpt-5.6-sol ", "gpt-6-astra", "gpt-5.6-sol", ""},
	)
	require.Equal(t, []string{"gpt-5.6-sol", "gpt-6-astra"}, got)
	require.Equal(t, []string{"gpt-6-astra", "gpt-5.6-sol"}, codexModelQualityDiagnosticModels([]string{"gpt-6-astra", "gpt-5.6-sol"}, nil))
}

func TestValidateCodexModelQualityDiagnosticModelsBoundsInput(t *testing.T) {
	require.NoError(t, validateCodexModelQualityDiagnosticModels([]string{"gpt-6-astra"}))
	for _, models := range [][]string{{""}, {string(make([]byte, 161))}} {
		err := validateCodexModelQualityDiagnosticModels(models)
		require.Equal(t, 400, infraerrors.Code(err))
	}
	tooMany := make([]string, 33)
	for i := range tooMany {
		tooMany[i] = "model-" + strconv.Itoa(i)
	}
	require.Equal(t, 400, infraerrors.Code(validateCodexModelQualityDiagnosticModels(tooMany)))
	require.NoError(t, validateCodexModelQualityDiagnosticModels([]string{"model", " model "}))
}

func TestCodexModelQualityDiagnosticAllowsDisabledPolicyWithoutChangingHash(t *testing.T) {
	s, _, job := qualityRuntimeFixture(t)
	job.policy.Enabled = false
	job.policyHash = codexModelQualityPolicyHash(job.policy)
	_, err := s.settingService.UpdateCodexModelQualityPolicy(context.Background(), job.policy)
	require.NoError(t, err)

	diagnosticJob, reason := s.prepareCodexModelQuality(withCodexModelQualityDiagnostic(context.Background()), job.account, job.ticket.Model, job.policy)
	require.NotNil(t, diagnosticJob, reason)
	require.True(t, diagnosticJob.diagnostic)
	require.False(t, diagnosticJob.policy.Enabled)
	require.Equal(t, codexModelQualityPolicyHash(job.policy), diagnosticJob.policyHash)
	diagnosticJob.diagnostic = true
	require.True(t, s.codexModelQualityCurrent(context.Background(), diagnosticJob))
}

func TestCodexModelQualityDiagnosticDoesNotQuarantineFailure(t *testing.T) {
	s, r, job := qualityRuntimeFixture(t)
	job.diagnostic = true
	status := qualityStatusForJob(job)
	status.Status, status.Reason = "suspect", "model_mismatch"
	s.finishCodexModelQuality(job, r, status, time.Now())
	require.NotNil(t, r.record)
	require.Equal(t, "suspect", r.record.Status.Status)
	require.Zero(t, r.writes)
}
