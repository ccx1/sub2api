package service

import (
	"bytes"
	"strings"
	"testing"
)

func TestOpenAICodexTicketResponseObserverCompletion(t *testing.T) {
	cases := []struct {
		name, body         string
		completed, matches bool
	}{
		{"SSE completed", reviewTicketEvent(`{"type":"response.completed","response":{"status":"completed","model":"gpt-test"}}`), true, true},
		{"SSE implicit status", reviewTicketEvent(`{"type":"response.completed","response":{"model":"gpt-test"}}`), true, true},
		{"SSE named event", "event: response.completed\r\ndata: {\"response\":{\"model\":\"gpt-test\"}}\r\n\r\n", true, true},
		{"SSE multiline CRLF", "event: response.completed\r\ndata: {\"type\":\"response.completed\",\r\ndata: \"response\":{\"status\":\"completed\",\"model\":\"gpt-test\"}}\r\n\r\n", true, true},
		{"JSON completed", `{"object":"response","status":"completed","model":"gpt-test"}`, true, true},
		{"different model", reviewTicketEvent(`{"type":"response.completed","response":{"model":"gpt-other"}}`), true, false},
		{"snapshot differs", `{"object":"response","status":"completed","model":"gpt-test-2026-09-01"}`, true, false},
		{"model whitespace differs", `{"object":"response","status":"completed","model":" gpt-test "}`, true, false},
		{"failed nested status", reviewTicketEvent(`{"type":"response.completed","response":{"status":"failed","model":"gpt-test"}}`), false, false},
		{"incomplete nested status", reviewTicketEvent(`{"type":"response.completed","response":{"status":"incomplete","model":"gpt-test"}}`), false, false},
		{"failed root status", reviewTicketEvent(`{"type":"response.completed","status":"failed","response":{"model":"gpt-test"}}`), false, false},
		{"nested error", reviewTicketEvent(`{"type":"response.completed","response":{"model":"gpt-test","error":{"code":"failed"}}}`), false, false},
		{"root error", reviewTicketEvent(`{"type":"response.completed","error":{},"response":{"model":"gpt-test"}}`), false, false},
		{"null errors allowed", reviewTicketEvent(`{"type":"response.completed","error":null,"response":{"model":"gpt-test","error":null}}`), true, true},
		{"created is not completed", reviewTicketEvent(`{"type":"response.created","response":{"status":"completed","model":"gpt-test"}}`), false, false},
		{"conflicting event type", "event: response.failed\ndata: {\"type\":\"response.completed\",\"response\":{\"model\":\"gpt-test\"}}\n\n", false, false},
		{"missing event type", reviewTicketEvent(`{"response":{"status":"completed","model":"gpt-test"}}`), false, false},
		{"missing model", reviewTicketEvent(`{"type":"response.completed","response":{"status":"completed"}}`), false, false},
		{"blank model", `{"object":"response","status":"completed","model":" "}`, false, false},
		{"failed JSON", `{"object":"response","status":"failed","model":"gpt-test"}`, false, false},
		{"JSON error", `{"object":"response","status":"completed","model":"gpt-test","error":false}`, false, false},
		{"truncated JSON", `{"object":"response","status":"completed","model":"gpt-test"`, false, false},
		{"truncated SSE framing", `data: {"type":"response.completed","response":{"model":"gpt-test"}}` + "\n", false, false},
		{"truncated SSE JSON", "data: {\"type\":\"response.completed\",\"response\":\n\n", false, false},
		{"trailing JSON", `{"object":"response","status":"completed","model":"gpt-test"}{}`, false, false},
		{"done marker", "data: [DONE]\n\n", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, size := range []int{1, 2, 7, 64, len(tc.body) + 1} {
				o := newOpenAICodexTicketResponseObserver("gpt-test")
				reviewObserveChunks(t, o, []byte(tc.body), size)
				o.Finish()
				if completed, matches := o.Result(); completed != tc.completed || matches != tc.matches {
					t.Fatalf("chunk=%d: got (%t,%t), want (%t,%t)", size, completed, matches, tc.completed, tc.matches)
				}
			}
		})
	}
}

func reviewTicketEvent(data string) string { return "data: " + data + "\n\n" }

func reviewObserveChunks(t *testing.T, o *openAICodexTicketResponseObserver, body []byte, size int) {
	t.Helper()
	for offset := 0; offset < len(body); offset += size {
		chunk := body[offset:min(offset+size, len(body))]
		before := bytes.Clone(chunk)
		o.Observe(chunk)
		if !bytes.Equal(before, chunk) {
			t.Fatal("observer changed bytes belonging to the forwarded response")
		}
	}
}

func TestOpenAICodexTicketResponseObserverDetectsBeforeEOF(t *testing.T) {
	o := newOpenAICodexTicketResponseObserver("gpt-test")
	o.Observe([]byte(reviewTicketEvent(`{"type":"response.completed","response":{"model":"gpt-other"}}`)))
	if complete, matches := o.Result(); !complete || matches {
		t.Fatal("completed model mismatch must be visible before EOF")
	}
	o.Observe([]byte(reviewTicketEvent(`{"type":"response.completed","response":{"model":"gpt-test"}}`)))
	o.Finish()
	if complete, matches := o.Result(); !complete || matches {
		t.Fatal("later matching completion masked an earlier mismatch")
	}
}

func TestOpenAICodexTicketResponseObserverFailureTakesPrecedence(t *testing.T) {
	for _, failure := range []string{
		`{"type":"error","error":{"code":"failed"}}`,
		`{"type":"response.failed","response":{"model":"gpt-test"}}`,
		`{"type":"response.incomplete","response":{"status":"incomplete"}}`,
		`{"type":"response.completed","response":{"model":"gpt-test","error":{}}}`,
	} {
		t.Run(failure, func(t *testing.T) {
			for _, failFirst := range []bool{true, false} {
				o := newOpenAICodexTicketResponseObserver("gpt-test")
				events := []string{failure, `{"type":"response.completed","response":{"model":"gpt-test"}}`}
				if !failFirst {
					events[0], events[1] = events[1], events[0]
				}
				for _, event := range events {
					o.Observe([]byte(reviewTicketEvent(event)))
				}
				o.Finish()
				if complete, matches := o.Result(); complete || matches {
					t.Fatal("failed response must never validate or be reported as a model mismatch")
				}
			}
		})
	}
}

func TestOpenAICodexTicketResponseObserverOversize(t *testing.T) {
	largeJSON := `{"object":"response","status":"completed","model":"gpt-test","extra":"` + strings.Repeat("x", 1<<20) + `"}`
	largeEvent := `{"type":"response.completed","response":{"model":"gpt-test"},"extra":"` + strings.Repeat("x", 1<<20) + `"}`
	cases := map[string]string{
		"JSON":            largeJSON,
		"JSON whitespace": strings.Repeat(" ", 1<<20) + `{"object":"response","status":"completed","model":"gpt-test"}`,
		"SSE single line": reviewTicketEvent(largeEvent),
		"SSE multiline":   "data: {\"type\":\"response.completed\",\n" + strings.Repeat("data: \"unused\":0,\n", 90000) + "data: \"response\":{\"model\":\"gpt-test\"}}\n\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			o := newOpenAICodexTicketResponseObserver("gpt-test")
			reviewObserveChunks(t, o, []byte(body), 8192)
			o.Finish()
			if complete, matches := o.Result(); complete || matches {
				t.Fatal("oversized document/event validated a ticket")
			}
		})
	}
}

func TestOpenAICodexTicketResponseObserverRecoversAfterOversizeEvent(t *testing.T) {
	o := newOpenAICodexTicketResponseObserver("gpt-test")
	o.Observe([]byte("data: " + strings.Repeat("x", (1<<20)+1) + "\n\n"))
	o.Observe([]byte(reviewTicketEvent(`{"type":"response.completed","response":{"model":"gpt-test"}}`)))
	if complete, matches := o.Result(); !complete || !matches {
		t.Fatal("a complete event after an oversized event must still be observed")
	}
}

func TestOpenAICodexTicketResponseObserverRejectsEmptyExpectedModel(t *testing.T) {
	for _, expected := range []string{"", " "} {
		o := newOpenAICodexTicketResponseObserver(expected)
		o.Observe([]byte(reviewTicketEvent(`{"type":"response.completed","response":{"model":"gpt-test"}}`)))
		o.Finish()
		if _, matches := o.Result(); matches {
			t.Fatal("empty expected model accepted a response")
		}
	}
}
