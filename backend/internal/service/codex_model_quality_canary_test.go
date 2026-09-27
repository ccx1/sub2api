package service

import (
	"context"
	"encoding/json"
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

func TestCodexQualityCanaryExactMatchesFinalAnswer(t *testing.T) {
	for _, tc := range []struct {
		answer string
		match  bool
	}{
		{"21", true}, {" 21。", true}, {"**21**", true}, {"答案：21", true}, {"Answer: 21 candies", true},
		{"21 颗", true}, {"推理过程……\n最终答案：21", true}, {"21.0", true},
		{"121", false}, {"210", false}, {"-21", false}, {"不是 21，是 18", false}, {"20 或 21", false},
		{"21\n修正：18", false}, {"", false}, {"twenty-one", false},
	} {
		require.Equal(t, tc.match, codexQualityCanaryExactMatches(tc.answer, []string{"21"}), tc.answer)
	}
	require.True(t, codexQualityCanaryExactMatches("Paris.", []string{"paris"}))
	require.False(t, codexQualityCanaryExactMatches("Paris, France", []string{"paris"}))
	require.True(t, codexQualityCanaryAnswerMatches("不是 21，是 18", []string{"21"}, ""), "contains mode keeps the old behavior")
	require.False(t, codexQualityCanaryAnswerMatches("不是 21，是 18", []string{"21"}, codexQualityCanaryExact))
}

func TestCodexModelQualityCanaryMatchPolicyValidation(t *testing.T) {
	p := DefaultCodexModelQualityPolicy()
	p.CanaryEnabled, p.CanaryPrompt, p.CanaryExpected = true, "candy", []string{"21"}
	for _, mode := range []string{"", codexQualityCanaryContains, codexQualityCanaryExact} {
		p.CanaryMatch = mode
		require.NoError(t, validateCodexModelQualityPolicy(p))
	}
	p.CanaryMatch = "regex"
	require.Error(t, validateCodexModelQualityPolicy(p))

	// 新字段为零值时不写入 JSON，已有策略的哈希和检测结论保持不变。
	legacy := DefaultCodexModelQualityPolicy()
	raw, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "canary_match")
	require.NotContains(t, string(raw), "canary_only")
}

func TestCodexModelQualityCanaryOnlySkipsCapabilityAndFingerprint(t *testing.T) {
	for _, tc := range []struct {
		name, status, reason string
		answers              []string
		calls                int
	}{
		{name: "pass", answers: []string{"21"}, status: "passed", reason: "canary_passed", calls: 1},
		{name: "retry_pass", answers: []string{"18", "21"}, status: "passed", reason: "canary_passed", calls: 2},
		{name: "degraded", answers: []string{"121", "不是 21，是 18"}, status: "suspect", reason: "canary_failed", calls: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, job := qualityRuntimeFixture(t)
			job.policy.FingerprintEnabled = true
			job.policy.CanaryEnabled, job.policy.CanaryOnly = true, true
			job.policy.CanaryPrompt, job.policy.CanaryExpected, job.policy.CanaryMatch = "candy", []string{"21"}, codexQualityCanaryExact
			calls := 0
			s.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				calls++
				body, _ := io.ReadAll(req.Body)
				require.Contains(t, string(body), "Output only the final answer")
				require.Contains(t, string(body), "candy")
				answer := qualityResponseBody(job.ticket.Model, tc.answers[calls-1])
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(answer))}, nil
			}}
			status := s.evaluateCodexModelQuality(context.Background(), job, CodexModelQualityStatus{})
			require.Equal(t, tc.calls, calls)
			require.Equal(t, tc.status, status.Status)
			require.Equal(t, tc.reason, status.Reason)
			require.Nil(t, status.CapabilityScore)
			require.Zero(t, status.SampleCount)
		})
	}
}
