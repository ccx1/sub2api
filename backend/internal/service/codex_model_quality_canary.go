package service

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	codexQualityCanaryContains = "contains"
	codexQualityCanaryExact    = "exact"
)

var codexQualityCanaryNumber = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

// normalizeCodexQualityCanaryText lowercases and collapses whitespace so that
// expected canary answers match regardless of case, line breaks or spacing.
func normalizeCodexQualityCanaryText(text string) string {
	return strings.Join(strings.Fields(strings.ToLower(text)), " ")
}

func codexQualityCanaryAnswerMatches(text string, expected []string, mode string) bool {
	if mode == codexQualityCanaryExact {
		return codexQualityCanaryExactMatches(text, expected)
	}
	return codexQualityCanaryMatches(text, expected)
}

// codexQualityCanaryFinalAnswer keeps the last non-empty line, drops a leading
// "答案：" / "Answer:" style label and surrounding punctuation or markdown.
func codexQualityCanaryFinalAnswer(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	answer := ""
	for i := len(lines) - 1; i >= 0 && answer == ""; i-- {
		answer = strings.TrimSpace(lines[i])
	}
	if i := strings.LastIndexAny(answer, ":：="); i >= 0 {
		_, size := utf8.DecodeRuneInString(answer[i:])
		answer = answer[i+size:]
	}
	decoration := func(r rune) bool { return unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) }
	// 左侧保留负号，避免 "-21" 被当成 21。
	answer = strings.TrimLeftFunc(answer, func(r rune) bool { return r != '-' && decoration(r) })
	answer = strings.TrimRightFunc(answer, decoration)
	return normalizeCodexQualityCanaryText(answer)
}

// codexQualityCanaryExactMatches requires the final answer to be exactly one
// expected answer. A numeric expected answer also accepts a unit ("21 颗"),
// but only when the final answer contains that single number, so "121",
// "210" or "不是 21，是 18" all miss.
func codexQualityCanaryExactMatches(text string, expected []string) bool {
	answer := codexQualityCanaryFinalAnswer(text)
	if answer == "" {
		return false
	}
	numbers := codexQualityCanaryNumber.FindAllString(answer, -1)
	for _, value := range expected {
		want := codexQualityCanaryFinalAnswer(value)
		if want == "" {
			continue
		}
		if answer == want {
			return true
		}
		wantNumber, err := strconv.ParseFloat(want, 64)
		if err != nil || len(numbers) != 1 {
			continue
		}
		if got, err := strconv.ParseFloat(numbers[0], 64); err == nil && got == wantNumber {
			return true
		}
	}
	return false
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
		output, err := s.requestCodexModelQualityWithInstructions(ctx, job, codexQualityAnswerInstructions, job.policy.CanaryPrompt)
		status.Requests++
		if err != nil {
			return codexQualityIncomplete(status, err)
		}
		mergeCodexQualityIdentity(&status, output.identity)
		if status.ModelIdentity == "mismatch" {
			status.Status, status.Reason = "suspect", "model_mismatch"
			return status
		}
		if codexQualityCanaryAnswerMatches(output.text, job.policy.CanaryExpected, job.policy.CanaryMatch) {
			status.Status, status.Reason = "passed", "canary_passed"
			return status
		}
	}
	status.Status, status.Reason = "suspect", "canary_failed"
	return status
}
