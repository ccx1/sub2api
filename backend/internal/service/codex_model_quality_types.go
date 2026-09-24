package service

import (
	"bytes"
	"context"
	"encoding/json"
	"time"
)

// 策略独立于采票配置，修改抽检规则不会使已发布票据失效。
type CodexModelQualityPolicy struct {
	Enabled                        bool   `json:"enabled"`
	IntervalSeconds                int    `json:"interval_seconds"`
	TimeoutSeconds                 int    `json:"timeout_seconds"`
	ReserveSeconds                 int    `json:"reserve_seconds"`
	MaxTTLPercent                  int    `json:"max_ttl_percent"`
	Concurrency                    int    `json:"concurrency"`
	AccountConcurrency             int    `json:"account_concurrency"`
	RetryIntervalSeconds           int    `json:"retry_interval_seconds"`
	LowQualityConsecutiveThreshold int    `json:"low_quality_consecutive_threshold"`
	LowQualityCooldownSeconds      int    `json:"low_quality_cooldown_seconds"`
	ReplacementCheckDelaySeconds   int    `json:"replacement_check_delay_seconds"`
	QuarantineOnFailure            bool   `json:"quarantine_on_failure"`
	FingerprintEnabled             bool   `json:"fingerprint_enabled"`
	ReasoningEffort                string `json:"reasoning_effort"`
	// ModelPriorities controls automatic quality-check order. Larger values
	// run first; models absent from the map keep the configured model order.
	ModelPriorities map[string]int `json:"model_priorities,omitempty"`
}

// UnmarshalJSON keeps requests and stored policies from before the per-account
// limit was added compatible with the old total-only concurrency behavior.
func (p *CodexModelQualityPolicy) UnmarshalJSON(data []byte) error {
	type policyAlias CodexModelQualityPolicy
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	value := policyAlias(*p)
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*p = CodexModelQualityPolicy(value)
	if _, exists := fields["account_concurrency"]; !exists {
		p.AccountConcurrency = p.Concurrency
	}
	return nil
}

// 仅保存诊断摘要；不包含票据、Cookie、代理地址、题目或业务正文。
type CodexModelQualityStatus struct {
	AccountID              int64      `json:"account_id"`
	Model                  string     `json:"model"`
	Status                 string     `json:"status"`
	Reason                 string     `json:"reason"`
	CheckedAt              *time.Time `json:"checked_at,omitempty"`
	NextCheckAt            *time.Time `json:"next_check_at,omitempty"`
	DurationMS             int64      `json:"duration_ms,omitempty"`
	TicketCapturedAt       *time.Time `json:"ticket_captured_at,omitempty"`
	TicketExpiresAt        *time.Time `json:"ticket_expires_at,omitempty"`
	CapabilityScore        *float64   `json:"capability_score,omitempty"`
	FingerprintCandidate   string     `json:"fingerprint_candidate,omitempty"`
	FingerprintProbability *float64   `json:"fingerprint_probability,omitempty"`
	FingerprintSimilarity  *float64   `json:"fingerprint_similarity,omitempty"`
	SampleCount            int        `json:"sample_count"`
	Requests               int        `json:"requests"`
	ModelIdentity          string     `json:"model_identity"`
	Source                 string     `json:"source"`
	BaselineReused         bool       `json:"baseline_reused,omitempty"`
	ConsecutiveLowQuality  int        `json:"consecutive_low_quality,omitempty"`
	QualityPausedUntil     *time.Time `json:"quality_paused_until,omitempty"`
}

type CodexModelQualityRecord struct {
	Status                CodexModelQualityStatus `json:"status"`
	Scope                 string                  `json:"scope"`
	Policy                string                  `json:"policy"`
	ConsecutiveLowQuality int                     `json:"consecutive_low_quality,omitempty"`
	QualityPausedUntil    *time.Time              `json:"quality_paused_until,omitempty"`
	LastLowQualityTicket  string                  `json:"last_low_quality_ticket,omitempty"`
}

type CodexModelQualityStore interface {
	LoadCodexModelQuality(context.Context, int64, string) (*CodexModelQualityRecord, error)
	AcquireCodexModelQuality(context.Context, int64, string, string, time.Duration) (bool, error)
	SaveCodexModelQuality(context.Context, int64, string, string, *CodexModelQualityRecord, time.Duration) (bool, error)
	ReleaseCodexModelQuality(context.Context, int64, string, string) error
}

type CodexModelQualityScheduleResult struct {
	Scheduled bool   `json:"scheduled"`
	Reason    string `json:"reason"`
}
