package service

import "strings"

func validCodexTicketOutcome(value string) bool {
	switch value {
	case "success", "ticket_rejected", "model_mismatch", "model_failed", "upstream_error", "verification_failed", "verification_deferred", "canceled", "controls_changed", "failed":
		return true
	}
	return false
}

func codexTicketAttemptOutcome(attempt *CodexTicketAttempt) string {
	switch {
	case attempt.Success:
		return "success"
	case attempt.canceled:
		return "canceled"
	case attempt.Reason == "controls_changed":
		return "controls_changed"
	case attempt.Reason == "verification_deferred":
		return "verification_deferred"
	case attempt.Reason == "harvest_protection_rejected":
		return "ticket_rejected"
	case strings.HasPrefix(attempt.Reason, "business_"):
		return "verification_failed"
	case strings.Contains(attempt.Reason, "model_mismatch"):
		return "model_mismatch"
	case strings.Contains(attempt.Reason, "http_rejected") || strings.Contains(attempt.Reason, "transport_failed"):
		return "upstream_error"
	case strings.Contains(attempt.Reason, "ticket_"):
		return "ticket_rejected"
	case strings.Contains(attempt.Reason, "response_"):
		return "model_failed"
	default:
		return "failed"
	}
}
