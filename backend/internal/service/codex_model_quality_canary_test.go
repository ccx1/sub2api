package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexQualityCanaryMatchesNormalizedAnswers(t *testing.T) {
	require.True(t, codexQualityCanaryMatches("The answer is\n  PARIS.", []string{"paris"}))
	require.True(t, codexQualityCanaryMatches("x = 42", []string{"41", "X  =  42"}))
	require.False(t, codexQualityCanaryMatches("London", []string{"paris"}))
	require.False(t, codexQualityCanaryMatches("   ", []string{"paris"}))
	require.False(t, codexQualityCanaryMatches("paris", []string{"  "}))
}

func TestCodexModelQualityCanaryPolicyValidation(t *testing.T) {
	p := DefaultCodexModelQualityPolicy()
	require.False(t, p.CanaryEnabled)
	require.NoError(t, validateCodexModelQualityPolicy(p))

	p.CanaryEnabled = true
	require.Error(t, validateCodexModelQualityPolicy(p))
	p.CanaryPrompt, p.CanaryExpected = "What is 6*7?", []string{"42"}
	require.NoError(t, validateCodexModelQualityPolicy(p))

	p.CanaryExpected = []string{"1", "2", "3", "4", "5", "6"}
	require.Error(t, validateCodexModelQualityPolicy(p))
	p.CanaryExpected = []string{strings.Repeat("a", codexQualityCanaryExpectedMaxBytes+1)}
	require.Error(t, validateCodexModelQualityPolicy(p))
	p.CanaryExpected, p.CanaryPrompt = []string{"42"}, strings.Repeat("a", codexQualityCanaryPromptMaxBytes+1)
	require.Error(t, validateCodexModelQualityPolicy(p))

	normalized := normalizeCodexModelQualityCanary(CodexModelQualityPolicy{CanaryPrompt: "  q  ", CanaryExpected: []string{" 42 ", "", "42", "Forty  Two", "forty two"}})
	require.Equal(t, "q", normalized.CanaryPrompt)
	require.Equal(t, []string{"42", "Forty  Two"}, normalized.CanaryExpected)
}

func TestCodexModelQualityCanaryFailureIsConfirmed(t *testing.T) {
	require.True(t, CodexModelQualityFailure(CodexModelQualityStatus{Status: "suspect", Reason: "canary_failed"}))
	require.False(t, CodexModelQualityFailure(CodexModelQualityStatus{Status: "passed", Reason: "canary_passed"}))
	require.True(t, codexTicketConfirmedQualityFailure("model_quality_canary_failed"))
}

func TestCodexModelQualityCanaryEvaluation(t *testing.T) {
	for _, tc := range []struct {
		name, reason, status string
		answers              []string
		err                  error
		calls                int
	}{
		{name: "hit", answers: []string{"The answer is 42."}, status: "passed", reason: "canary_passed", calls: 1},
		{name: "retry_hit", answers: []string{"forty", "42"}, status: "passed", reason: "canary_passed", calls: 2},
		{name: "miss", answers: []string{"41", "43"}, status: "suspect", reason: "canary_failed", calls: 2},
		{name: "network", err: errors.New("boom"), status: "inconclusive", reason: "network_error", calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, job := qualityRuntimeFixture(t)
			job.policy.CanaryEnabled, job.policy.CanaryPrompt, job.policy.CanaryExpected = true, "What is 6*7?", []string{"42"}
			calls := 0
			s.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				calls++
				if tc.err != nil {
					return nil, tc.err
				}
				body := qualityResponseBody(job.ticket.Model, tc.answers[calls-1])
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
			}}
			status := s.evaluateCodexQualityCanary(context.Background(), job, CodexModelQualityStatus{Status: "passed", Reason: "capability_passed"})
			require.Equal(t, tc.calls, calls)
			require.Equal(t, tc.calls, status.Requests)
			require.Equal(t, tc.status, status.Status)
			require.Equal(t, tc.reason, status.Reason)
		})
	}
}
