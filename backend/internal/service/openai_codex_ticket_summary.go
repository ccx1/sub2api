package service

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/codextickettrace"
)

type CodexTicketNetwork = codextickettrace.Snapshot

type CodexTicketModelDeclaration struct {
	FirstModel    string `json:"first_model,omitempty"`
	FirstEvent    string `json:"first_event,omitempty"`
	TerminalModel string `json:"terminal_model,omitempty"`
	TerminalEvent string `json:"terminal_event,omitempty"`
	Conflict      *bool  `json:"conflict,omitempty"`
	Truncated     bool   `json:"truncated,omitempty"`
}

type CodexTicketUpstreamError struct {
	WireHTTPStatus       int        `json:"wire_http_status"`
	EffectiveStatus      int        `json:"effective_status"`
	Type                 string     `json:"type,omitempty"`
	Code                 string     `json:"code,omitempty"`
	Scope                string     `json:"scope,omitempty"`
	RetryAt              *time.Time `json:"retry_at,omitempty"`
	ClassificationSource string     `json:"classification_source,omitempty"`
}

type CodexTicketQuotaSignal struct {
	ID                string     `json:"id"`
	UsedPercent       *float64   `json:"used_percent,omitempty"`
	ResetAt           *time.Time `json:"reset_at,omitempty"`
	ResetAfterSeconds *int64     `json:"reset_after_seconds,omitempty"`
	WindowMinutes     *int64     `json:"window_minutes,omitempty"`
	LimitReached      *bool      `json:"limit_reached,omitempty"`
}

type CodexTicketSignals struct {
	SafetyBufferingEnabled *bool                    `json:"safety_buffering_enabled,omitempty"`
	FasterModel            string                   `json:"faster_model,omitempty"`
	ActiveLimit            string                   `json:"active_limit,omitempty"`
	PlanType               string                   `json:"plan_type,omitempty"`
	Quota                  []CodexTicketQuotaSignal `json:"quota,omitempty"`
	Truncated              bool                     `json:"truncated,omitempty"`
}
