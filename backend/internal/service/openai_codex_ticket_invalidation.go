package service

import "time"

const OpenAICodexTicketInvalidationsKey = openAICodexTicketExtraKeyPrefix + "invalidations"
const OpenAICodexTicketInvalidationsLimit = 100

// 独立保存撤销事件，避免票发布后、采集记录落库前发生撤销而丢失原因。
// 不包含业务正文、认证信息或票据内容。
type CodexTicketInvalidation struct {
	AttemptID            string              `json:"attempt_id"`
	Model                string              `json:"model"`
	CapturedAt           time.Time           `json:"captured_at"`
	InvalidatedAt        time.Time           `json:"invalidated_at"`
	Reason               string              `json:"reason"`
	Source               string              `json:"source"`
	ReportedModels       []string            `json:"reported_models,omitempty"`
	ReturnedTicketLength *int                `json:"returned_ticket_length,omitempty"`
	Signals              *CodexTicketSignals `json:"signals,omitempty"`
}

func newCodexTicketInvalidation(reason, source string, models []string) *CodexTicketInvalidation {
	return &CodexTicketInvalidation{InvalidatedAt: time.Now(), Reason: reason, Source: source,
		ReportedModels: append([]string(nil), models...)}
}

func codexTicketRejectedInvalidation(source, state string) *CodexTicketInvalidation {
	event := newCodexTicketInvalidation("response_ticket_rejected", source, nil)
	length := len(state)
	event.ReturnedTicketLength = &length
	return event
}

func codexTicketWithInvalidation(used *openAICodexTicket, detail *CodexTicketInvalidation) *openAICodexTicket {
	if used == nil {
		return nil
	}
	if detail == nil {
		detail = newCodexTicketInvalidation("unknown", "unknown", nil)
	}
	event := *detail
	event.AttemptID, event.Model, event.CapturedAt = used.AttemptID, used.Model, used.CapturedAt
	event.ReportedModels = append([]string(nil), detail.ReportedModels...)
	event.Signals = cloneCodexTicketSignals(detail.Signals)
	ticket := *used
	ticket.Invalidation = &event
	return &ticket
}
