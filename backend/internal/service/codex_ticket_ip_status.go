package service

import (
	"context"
	"time"
)

// CodexIPStatusKind identifies a Codex ticket protection state that is still
// blocking an egress IP from being selected.
type CodexIPStatusKind string

const (
	CodexIPStatusCooling  CodexIPStatusKind = "cooling"
	CodexIPStatusDisabled CodexIPStatusKind = "disabled"
)

// CodexIPStatus is the redacted, administrative view of a protected egress IP.
// The Redis key contains only a hash; the IP is read from the state payload and
// legacy states without that field are intentionally omitted by the reader.
type CodexIPStatus struct {
	IP             string            `json:"ip"`
	Status         CodexIPStatusKind `json:"status"`
	UntilAt        *time.Time        `json:"until_at,omitempty"`
	Rounds         int               `json:"rounds"`
	FailedAccounts int               `json:"failed_accounts"`
	LastFailureAt  *time.Time        `json:"last_failure_at,omitempty"`
}

// CodexIPStatusReader provides a read-only view over the scheduler's IP
// protection state. Implementations must not alter or expire Redis state.
type CodexIPStatusReader interface {
	ListCodexIPStatus(ctx context.Context, status CodexIPStatusKind, page, pageSize int) ([]CodexIPStatus, int64, error)
}
