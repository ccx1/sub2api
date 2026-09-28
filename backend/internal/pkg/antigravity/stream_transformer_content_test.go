package antigravity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStreamingProcessorHasContent(t *testing.T) {
	for _, tc := range []struct {
		name, part string
		want       bool
	}{
		{"signature_only", `{"text":"","thoughtSignature":"sig"}`, false},
		{"text_with_signature", `{"text":"answer","thoughtSignature":"sig"}`, true},
		{"text_only", `{"text":"answer"}`, true},
		{"thinking_with_signature", `{"text":"reason","thought":true,"thoughtSignature":"sig"}`, true},
		{"thinking_signature_only", `{"text":"","thought":true,"thoughtSignature":"sig"}`, false},
		{"function_call", `{"functionCall":{"name":"lookup","args":{}}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			processor := NewStreamingProcessor("gemini-3.7-flash")
			line := `data: {"response":{"candidates":[{"content":{"role":"model","parts":[` + tc.part + `]},"finishReason":"MALFORMED_FUNCTION_CALL"}]}}`
			events := processor.ProcessLine(line)
			require.NotEmpty(t, events)
			if tc.name == "text_with_signature" {
				require.Contains(t, string(events), `"type":"text_delta"`)
				require.Contains(t, string(events), `"text":"answer"`)
			}
			require.Equal(t, tc.want, processor.HasContent())
			processor.Finish()
			require.Equal(t, tc.want, processor.HasContent())
		})
	}
}
