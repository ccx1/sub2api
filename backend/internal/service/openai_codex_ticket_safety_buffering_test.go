package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func safetyBufferingBoolPtr(b bool) *bool { return &b }

func TestCodexSafetyBufferingRejectReason(t *testing.T) {
	enabled := &CodexTicketSignals{SafetyBufferingEnabled: safetyBufferingBoolPtr(true)}
	faster := &CodexTicketSignals{FasterModel: "gpt-5.6-luna"}
	both := &CodexTicketSignals{SafetyBufferingEnabled: safetyBufferingBoolPtr(true), FasterModel: "gpt-5.6-luna"}
	clean := &CodexTicketSignals{SafetyBufferingEnabled: safetyBufferingBoolPtr(false)}

	cases := []struct {
		name    string
		mode    string
		signals *CodexTicketSignals
		want    string
	}{
		{"off ignores everything", config.CodexTicketRejectSafetyBufferingOff, both, ""},
		{"empty mode ignores everything", "", both, ""},
		{"nil signals never reject", config.CodexTicketRejectSafetyBufferingAny, nil, ""},
		{"any rejects enabled", config.CodexTicketRejectSafetyBufferingAny, enabled, "safety_buffering_flagged"},
		{"any rejects faster model", config.CodexTicketRejectSafetyBufferingAny, faster, "safety_buffering_faster_model"},
		{"any passes clean", config.CodexTicketRejectSafetyBufferingAny, clean, ""},
		{"faster_only ignores enabled without faster model", config.CodexTicketRejectSafetyBufferingFasterModel, enabled, ""},
		{"faster_only rejects faster model", config.CodexTicketRejectSafetyBufferingFasterModel, faster, "safety_buffering_faster_model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := codexSafetyBufferingRejectReason(tc.mode, tc.signals); got != tc.want {
				t.Fatalf("mode=%q got=%q want=%q", tc.mode, got, tc.want)
			}
		})
	}
}

func TestCodexSafetyBufferingReasonConfirmed(t *testing.T) {
	for _, r := range []string{"safety_buffering_flagged", "safety_buffering_faster_model"} {
		if !codexSafetyBufferingReasonConfirmed(r) {
			t.Fatalf("reason %q should be confirmed", r)
		}
	}
	for _, r := range []string{"", "model_mismatch", "capability_failed"} {
		if codexSafetyBufferingReasonConfirmed(r) {
			t.Fatalf("reason %q should not be confirmed by buffering check", r)
		}
	}
}
