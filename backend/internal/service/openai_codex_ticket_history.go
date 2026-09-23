package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

const OpenAICodexTicketHistoryKey = openAICodexTicketExtraKeyPrefix + "activity"
const OpenAICodexTicketHistoryLimit = 100
const OpenAICodexTicketExchangeHistoryLimit = 10

type CodexTicketProxySnapshot struct {
	ID      int64  `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
}

type CodexTicketAttempt struct {
	canceled                   bool
	protectionLengthHit        bool
	Outcome                    string                    `json:"outcome,omitempty"`
	Protection                 *CodexTicketRuntimeStatus `json:"protection,omitempty"`
	ID                         string                    `json:"id"`
	StartedAt                  time.Time                 `json:"started_at"`
	FinishedAt                 time.Time                 `json:"finished_at"`
	Model                      string                    `json:"model"`
	Success                    bool                      `json:"success"`
	Reason                     string                    `json:"reason"`
	HarvestProxy               *CodexTicketProxySnapshot `json:"harvest_proxy"`
	BusinessProxy              *CodexTicketProxySnapshot `json:"business_proxy"`
	LengthMode                 string                    `json:"length_mode,omitempty"`
	TargetLength               *int                      `json:"target_length,omitempty"`
	RejectedLengths            []int                     `json:"rejected_lengths,omitempty"`
	HarvestTicketLength        *int                      `json:"harvest_ticket_length,omitempty"`
	BusinessTicketLength       *int                      `json:"business_ticket_length,omitempty"`
	HarvestHTTPStatus          *int                      `json:"harvest_http_status,omitempty"`
	BusinessHTTPStatus         *int                      `json:"business_http_status,omitempty"`
	BusinessVerificationRounds int                       `json:"business_verification_rounds,omitempty"`
	BusinessVerificationPassed int                       `json:"business_verification_passed,omitempty"`
	BusinessVerificationModels []string                  `json:"business_verification_models,omitempty"`
	HarvestExchange            *CodexTicketExchange      `json:"harvest_exchange,omitempty"`
	BusinessExchange           *CodexTicketExchange      `json:"business_exchange,omitempty"`
	TicketCapturedAt           *time.Time                `json:"ticket_captured_at,omitempty"`
	TicketExpiresAt            *time.Time                `json:"ticket_expires_at,omitempty"`
	TicketStatus               string                    `json:"ticket_status,omitempty"`
	Invalidation               *CodexTicketInvalidation  `json:"invalidation,omitempty"`
}

type CodexTicketAttemptSummary struct {
	OutcomeCounts           map[string]int64 `json:"outcome_counts,omitempty"`
	ClassificationStartedAt *time.Time       `json:"classification_started_at,omitempty"`
	Total                   int64            `json:"total"`
	Success                 int64            `json:"success"`
	Failed                  int64            `json:"failed"`
	LastAttemptAt           *time.Time       `json:"last_attempt_at,omitempty"`
}

type CodexTicketHistory struct {
	Summary               CodexTicketAttemptSummary       `json:"summary"`
	Items                 []CodexTicketAttempt            `json:"items"`
	Total                 int                             `json:"total"`
	Page                  int                             `json:"page"`
	PageSize              int                             `json:"page_size"`
	RetainedLimit         int                             `json:"retained_limit"`
	ExchangeRetainedLimit int                             `json:"exchange_retained_limit"`
	FilterOptions         CodexTicketHistoryFilterOptions `json:"filter_options"`
}

type codexTicketAttemptRecorder interface {
	RecordCodexTicketAttempt(context.Context, int64, CodexTicketAttempt) error
}

func DecodeCodexTicketHistory(raw any) (CodexTicketHistory, error) {
	history := CodexTicketHistory{Items: []CodexTicketAttempt{}}
	if raw == nil {
		return history, nil
	}
	encoded, err := json.Marshal(raw)
	if err == nil {
		err = json.Unmarshal(encoded, &history)
	}
	return history, err
}

// 调用者必须在同一账号的数据库行锁内读取和追加，避免多模型/多实例丢计数。
func (h *CodexTicketHistory) Append(attempt CodexTicketAttempt) {
	if attempt.Outcome != "" {
		if h.Summary.OutcomeCounts == nil {
			h.Summary.OutcomeCounts = make(map[string]int64)
		}
		h.Summary.OutcomeCounts[attempt.Outcome]++
		if h.Summary.ClassificationStartedAt == nil {
			started := attempt.StartedAt
			h.Summary.ClassificationStartedAt = &started
		}
	}
	h.Summary.Total++
	if attempt.Success {
		h.Summary.Success++
	} else {
		h.Summary.Failed++
	}
	if h.Summary.LastAttemptAt == nil || attempt.StartedAt.After(*h.Summary.LastAttemptAt) {
		started := attempt.StartedAt
		h.Summary.LastAttemptAt = &started
	}
	h.Items = append(h.Items, attempt)
	sort.SliceStable(h.Items, func(i, j int) bool { return h.Items[i].StartedAt.After(h.Items[j].StartedAt) })
	if len(h.Items) > OpenAICodexTicketHistoryLimit {
		h.Items = h.Items[:OpenAICodexTicketHistoryLimit]
	}
	// 旧记录继续保留模型诊断，只清理较早的报文，限制账号 JSON 的体积。
	for i := OpenAICodexTicketExchangeHistoryLimit; i < len(h.Items); i++ {
		for _, exchange := range []**CodexTicketExchange{&h.Items[i].HarvestExchange, &h.Items[i].BusinessExchange} {
			if *exchange != nil {
				summary := **exchange
				summary.Request, summary.Response = nil, nil
				*exchange = &summary
			}
		}
	}
}

func GetCodexTicketHistory(account *Account, page, size int, filters ...CodexTicketHistoryFilter) (CodexTicketHistory, error) {
	history, err := DecodeCodexTicketHistory(account.Extra[OpenAICodexTicketHistoryKey])
	if err != nil {
		return history, err
	}
	if err := projectCodexTicketHistory(&history, account, time.Now()); err != nil {
		return CodexTicketHistory{}, err
	}
	history.FilterOptions = codexTicketHistoryFilterOptions(history.Items)
	if len(filters) > 0 {
		items := make([]CodexTicketAttempt, 0, len(history.Items))
		for _, item := range history.Items {
			if filters[0].matches(item) {
				items = append(items, item)
			}
		}
		history.Items = items
	}
	history.Page, history.PageSize = max(1, page), max(1, min(100, size))
	history.RetainedLimit = OpenAICodexTicketHistoryLimit
	history.ExchangeRetainedLimit = OpenAICodexTicketExchangeHistoryLimit
	history.Total = len(history.Items)
	// 先限制页码再乘法，避免用户输入超大 page 造成整数溢出。
	start := min(history.Page-1, history.Total) * history.PageSize
	start = min(start, history.Total)
	end := min(start+history.PageSize, history.Total)
	history.Items = append([]CodexTicketAttempt{}, history.Items[start:end]...)
	return history, nil
}

func codexTicketProxySnapshot(raw string, id int64, name string) *CodexTicketProxySnapshot {
	address := "direct"
	if strings.TrimSpace(raw) != "" {
		address = "unavailable"
		if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
			address = parsed.Scheme + "://" + parsed.Host
		}
	}
	return &CodexTicketProxySnapshot{ID: id, Name: name, Address: address}
}

func newCodexTicketAttempt(model string) *CodexTicketAttempt {
	return &CodexTicketAttempt{ID: uuid.NewString(), Model: model, Reason: "harvest_failed"}
}

func codexTicketAttemptErrorReason(stage string, err error) string {
	var rejected *openAICodexTicketProbeRejected
	if errors.As(err, &rejected) {
		return stage + "_http_rejected"
	}
	var transport *codexTicketTransportError
	if errors.As(err, &transport) {
		return stage + "_transport_failed"
	}
	var response *codexTicketProbeResponseError
	if errors.As(err, &response) {
		return stage + "_" + response.reason
	}
	return stage + "_failed"
}

func (s *OpenAIGatewayService) finishCodexTicketAttempt(ctx context.Context, id int64, attempt *CodexTicketAttempt) {
	if attempt.StartedAt.IsZero() {
		return
	}
	recorder, ok := s.accountRepo.(codexTicketAttemptRecorder)
	if !ok {
		return
	}
	attempt.FinishedAt = time.Now()
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := recorder.RecordCodexTicketAttempt(writeCtx, id, *attempt); err != nil {
		logger.L().Warn("openai_codex_ticket history persistence failed", zap.Int64("account_id", id))
	}
}
