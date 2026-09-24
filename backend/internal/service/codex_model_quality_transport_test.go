package service

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func qualityResponseBody(model, answer string) string {
	response := map[string]any{"status": "completed", "model": model, "output": []any{map[string]any{
		"type": "message", "content": []any{map[string]string{"type": "output_text", "text": answer}}}}}
	data, _ := json.Marshal(map[string]any{"type": "response.completed", "response": response})
	return "data: " + string(data) + "\n\n"
}

func TestCodexModelQualityResponseIdentityAndBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, body, identity, text string
		invalid                    bool
	}{
		{"matching", qualityResponseBody("gpt-6-astra", "[1]"), "match", "[1]", false},
		{"missing declaration", qualityResponseBody("", "[2]"), "unknown", "[2]", false},
		{"wrong model", qualityResponseBody("gpt-5.6-luna", "[3]"), "mismatch", "[3]", false},
		{"mismatch cannot recover", qualityResponseBody("gpt-5.6-luna", "[3]") + qualityResponseBody("gpt-6-astra", "[4]"), "mismatch", "[4]", false},
		{"failed then complete", "data: {\"type\":\"response.failed\"}\n\n" + qualityResponseBody("gpt-6-astra", "[1]"), "", "", true},
		{"truncated SSE", strings.TrimSuffix(qualityResponseBody("gpt-6-astra", "[1]"), "\n"), "", "", true},
		{"no terminal", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"[1]\"}\n\n", "", "", true},
		{"too large", strings.Repeat("x", codexModelQualityResponseLimit+1), "", "", true},
		{"JSON completed", `{"object":"response","status":"completed","output":[{"content":[{"type":"output_text","text":"{}"}]}]}`, "unknown", "{}", false},
		{"JSON not completed", `{"object":"response","model":"gpt-6-astra","output":[]}`, "", "", true},
		{"event error cannot declare mismatch", "event: error\n" + qualityResponseBody("other", "{}"), "", "", true},
		{"conflicting event", "event: response.created\n" + qualityResponseBody("other", "{}"), "", "", true},
		{"top level error", strings.Replace(qualityResponseBody("other", "{}"), `"type":"response.completed"`, `"error":{"message":"failed"},"type":"response.completed"`, 1), "", "", true},
		{"top level unfinished", strings.Replace(qualityResponseBody("other", "{}"), `"type":"response.completed"`, `"status":"in_progress","type":"response.completed"`, 1), "", "", true},
		{"intermediate nested error", "data: {\"type\":\"response.created\",\"response\":{\"error\":{\"message\":\"failed\"}}}\n\n" + qualityResponseBody("other", "{}"), "", "", true},
		{"cancelled then complete", "data: {\"type\":\"response.cancelled\"}\n\n" + qualityResponseBody("other", "{}"), "", "", true},
		{"nonstring model", strings.Replace(qualityResponseBody("other", "{}"), `"model":"other"`, `"model":42`, 1), "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			output, err := readCodexQualityOutput(strings.NewReader(tc.body), "gpt-6-astra")
			if tc.invalid {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.identity, output.identity)
			require.Equal(t, tc.text, output.text)
		})
	}
}

func TestCodexModelQualityResponseDoesNotDuplicateFinalOutput(t *testing.T) {
	delta := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"[1,2]\"}\n\n"
	for _, body := range []string{delta + qualityResponseBody("gpt-6-astra", "[1,2]"), delta + qualityResponseBody("gpt-6-astra", "")} {
		output, err := readCodexQualityOutput(strings.NewReader(body), "gpt-6-astra")
		require.NoError(t, err)
		require.Equal(t, "[1,2]", output.text)
	}
}

func TestCodexModelQualityMissingDeclarationKeepsExistingAdmissionStrict(t *testing.T) {
	body := qualityResponseBody("", "[1]")
	observer := newOpenAICodexTicketResponseObserver("gpt-6-astra")
	observer.Observe([]byte(body))
	observer.Finish()
	require.True(t, observer.protocolCompleted)
	completed, matches := observer.Result()
	require.False(t, completed)
	require.False(t, matches)
	require.ErrorContains(t, codexTicketProbeResponseResult(observer), "response_incomplete")
}
