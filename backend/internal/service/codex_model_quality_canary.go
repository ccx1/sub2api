package service

import (
	"context"
	"strings"
)

// normalizeCodexQualityCanaryText lowercases and collapses whitespace so that
// expected canary answers match regardless of case, line breaks or spacing.
func normalizeCodexQualityCanaryText(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(text)), " ")
}

func codexQualityCanaryMatches(text string, expected []string) bool {
	answer := normalizeCodexQualityCanaryText(text)
	if answer == "" {
		return false
	}
	for _, value := range expected {
		if want := normalizeCodexQualityCanaryText(value); want != "" && strings.Contains(answer, want) {
			return true
		}
	}
	return false
}

// evaluateCodexQualityCanary asks the administrator-defined canary question
// and retries once before confirming a miss, mirroring the capability recheck.
// Transport or empty-output errors stay inconclusive and never revoke tickets.
func (s *OpenAIGatewayService) evaluateCodexQualityCanary(ctx context.Context, job *codexModelQualityJob, status CodexModelQualityStatus) CodexModelQualityStatus {
	for attempt := 0; attempt < 2; attempt++ {
		output, err := s.requestCodexModelQuality(ctx, job, job.policy.CanaryPrompt)
		status.Requests++
		if err != nil {
			return codexQualityIncomplete(status, err)
		}
		mergeCodexQualityIdentity(&status, output.identity)
		if status.ModelIdentity == "mismatch" {
			status.Status, status.Reason = "suspect", "model_mismatch"
			return status
		}
		if codexQualityCanaryMatches(output.text, job.policy.CanaryExpected) {
			status.Status, status.Reason = "passed", "canary_passed"
			return status
		}
	}
	status.Status, status.Reason = "suspect", "canary_failed"
	return status
}
