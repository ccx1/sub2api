package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// CodexTicketScheduler 的准入、预算及恢复令牌由共享存储原子维护。
type CodexTicketScheduler interface {
	ReserveCodexTicket(context.Context, CodexTicketReserveRequest) (*CodexTicketReservation, error)
	StartCodexTicket(context.Context, *CodexTicketReservation) error
	CheckCodexTicketStage(context.Context, *CodexTicketReservation, *Proxy) error
	ValidateCodexTicket(context.Context, *CodexTicketReservation) error
	ReportCodexTicketHarvest(context.Context, *CodexTicketReservation, bool, bool, bool) error
	FinishCodexTicket(context.Context, CodexTicketFinishRequest) error
	GetCodexTicketRuntimeStatus(context.Context, int64) (*CodexTicketRuntimeStatus, error)
}

type CodexTicketReserveRequest struct {
	HarvestUsesBusiness bool
	AllowDirectOnEmpty  bool
	AccountID           int64
	Model               string
	Models              []string
	Selection           ProxyPoolSelection
	FixedProxy          *Proxy
	PoolMode            bool
	Strategy            string
	Config              config.OpenAICodexTicketConfig
	Manual              bool
}

type CodexTicketReservation struct {
	SessionEpoch string
	AccountID    int64
	Model        string
	Token        string
	Generation   int64
	Proxy        *Proxy
	HalfOpen     bool
	Manual       bool
	Status       *CodexTicketRuntimeStatus
	// 策略快照用于发送前复核，不作为原业务票据绑定的一部分。
	Config config.OpenAICodexTicketConfig
}

type CodexTicketSessionScope struct {
	AccountID int64
	Model     string
	Mode      string
}

type CodexTicketSessionEpochReader interface {
	GetCodexTicketSessionEpoch(context.Context, CodexTicketSessionScope) (string, error)
}

type CodexTicketFinishRequest struct {
	Reservation            *CodexTicketReservation
	Started                bool
	HarvestAccepted        bool
	HarvestProxyFailed     bool
	QualityProxyFailed     bool
	BusinessProxyFailed    bool
	BusinessProxySucceeded bool
	Silence                bool
	Outcome                string
	RetryAt                time.Time
	RetryScope             string
	TargetsComplete        bool
	DeferredProxyID        int64
	DeferredUntil          time.Time
}

type CodexTicketRuntimeStatus struct {
	State           string     `json:"state"`
	Reason          string     `json:"reason,omitempty"`
	Model           string     `json:"model,omitempty"`
	ProxyID         int64      `json:"proxy_id,omitempty"`
	RetryAt         *time.Time `json:"retry_at,omitempty"`
	CooldownUntil   *time.Time `json:"cooldown_until,omitempty"`
	AttemptsUsed    int        `json:"attempts_used"`
	MaxAttempts     int        `json:"max_attempts"`
	Round           int        `json:"round"`
	MaxRounds       int        `json:"max_rounds"`
	HalfOpen        bool       `json:"half_open"`
	Generation      int64      `json:"generation"`
	LastModel       string     `json:"last_model,omitempty"`
	RuleMatched     bool       `json:"rule_matched,omitempty"`
	SilenceUntil    *time.Time `json:"silence_until,omitempty"`
	PolicyVersion   string     `json:"policy_version,omitempty"`
	HarvestHalfOpen bool       `json:"harvest_half_open,omitempty"`
	HarvestAccepted bool       `json:"harvest_accepted,omitempty"`
}

// 等待与上游失败分开；等待不应增加失败计数或触发空池直连。
type CodexTicketWaitError struct{ Status *CodexTicketRuntimeStatus }

func (e *CodexTicketWaitError) Error() string {
	if e == nil || e.Status == nil {
		return "codex ticket scheduler unavailable"
	}
	return "codex ticket scheduler: " + e.Status.Reason
}
