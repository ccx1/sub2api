package service

import (
	"encoding/json"
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"
)

type CodexTicketHistoryFilter struct {
	Outcome      string
	Result       string
	TicketStatus string
	Model        string
	Reason       string
	StartedFrom  *time.Time
	StartedTo    *time.Time
}

type CodexTicketHistoryFilterOptions struct {
	Outcomes []string `json:"outcomes"`
	Models   []string `json:"models"`
	Reasons  []string `json:"reasons"`
}

func ParseCodexTicketHistoryFilter(query url.Values) (CodexTicketHistoryFilter, error) {
	filter := CodexTicketHistoryFilter{
		Outcome: strings.TrimSpace(query.Get("outcome")),
		Result:  strings.TrimSpace(query.Get("result")), TicketStatus: strings.TrimSpace(query.Get("ticket_status")),
		Model: strings.TrimSpace(query.Get("model")), Reason: strings.TrimSpace(query.Get("reason")),
	}
	if filter.Outcome != "" && !validCodexTicketOutcome(filter.Outcome) {
		return filter, errors.New("invalid outcome filter")
	}
	if filter.Result != "" && filter.Result != "success" && filter.Result != "failed" {
		return filter, errors.New("invalid result filter")
	}
	switch filter.TicketStatus {
	case "", "not_issued", "invalidated", "ttl_elapsed", "issued", "unknown":
	default:
		return filter, errors.New("invalid ticket status filter")
	}
	if len(filter.Model) > 256 || !validCodexTicketHistoryReason(filter.Reason) {
		return filter, errors.New("invalid model or reason filter")
	}
	for _, field := range []struct {
		name   string
		target **time.Time
	}{{"started_from", &filter.StartedFrom}, {"started_to", &filter.StartedTo}} {
		if raw := strings.TrimSpace(query.Get(field.name)); raw != "" {
			value, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return filter, errors.New("invalid " + field.name + " filter")
			}
			*field.target = &value
		}
	}
	if filter.StartedFrom != nil && filter.StartedTo != nil && filter.StartedFrom.After(*filter.StartedTo) {
		return filter, errors.New("started_from must not be after started_to")
	}
	return filter, nil
}

func validCodexTicketHistoryReason(reason string) bool {
	if reason == "" {
		return true
	}
	prefix, code, found := strings.Cut(reason, ":")
	if !found || (prefix != "attempt" && prefix != "invalidation") || code == "" || len(reason) > 160 {
		return false
	}
	for _, char := range code {
		if (char < 'a' || char > 'z') && (char < '0' || char > '9') && char != '_' {
			return false
		}
	}
	return true
}

func (f CodexTicketHistoryFilter) matches(item CodexTicketAttempt) bool {
	if f.Outcome != "" && item.Outcome != f.Outcome {
		return false
	}
	if (f.Result == "success" && !item.Success) || (f.Result == "failed" && item.Success) {
		return false
	}
	if f.TicketStatus != "" && item.TicketStatus != f.TicketStatus {
		return false
	}
	if f.Model != "" && strings.TrimSpace(item.Model) != f.Model {
		return false
	}
	if f.Reason != "" && codexTicketHistoryReason(item) != f.Reason {
		return false
	}
	return (f.StartedFrom == nil || !item.StartedAt.Before(*f.StartedFrom)) &&
		(f.StartedTo == nil || !item.StartedAt.After(*f.StartedTo))
}

func projectCodexTicketHistory(history *CodexTicketHistory, account *Account, now time.Time) error {
	events, err := codexTicketHistoryInvalidations(account)
	if err != nil {
		return err
	}
	history.reconcileQualityFailures(account, events, nil)
	for i := range history.Items {
		item := &history.Items[i]
		invalidation := codexTicketHistoryInvalidation(account, *item, events)
		item.Invalidation = nil
		item.TicketStatus = "not_issued"
		if !item.Success && !codexTicketConfirmedQualityFailure(item.Reason) {
			continue
		}
		item.Invalidation = invalidation
		switch {
		case item.Invalidation != nil:
			item.TicketStatus = "invalidated"
		case item.TicketCapturedAt == nil || item.TicketExpiresAt == nil ||
			item.TicketCapturedAt.IsZero() || item.TicketExpiresAt.IsZero() ||
			item.TicketExpiresAt.Before(*item.TicketCapturedAt):
			item.TicketStatus = "unknown"
		case !now.Before(*item.TicketExpiresAt):
			item.TicketStatus = "ttl_elapsed"
		default:
			// 已签发仅表示未观察到撤销，不能保证上游仍接受此票。
			item.TicketStatus = "issued"
		}
	}
	return nil
}

func matchingCodexTicketInvalidation(item CodexTicketAttempt, events []CodexTicketInvalidation) *CodexTicketInvalidation {
	if item.ID == "" || item.TicketCapturedAt == nil || item.TicketCapturedAt.IsZero() {
		return nil
	}
	var matched *CodexTicketInvalidation
	for _, event := range events {
		if event.AttemptID != item.ID || event.Model != item.Model || !event.CapturedAt.Equal(*item.TicketCapturedAt) ||
			event.InvalidatedAt.IsZero() || event.InvalidatedAt.Before(event.CapturedAt) {
			continue
		}
		if matched == nil || event.InvalidatedAt.Before(matched.InvalidatedAt) {
			copy := event
			matched = &copy
		}
	}
	return matched
}

func codexTicketHistoryTombstoneInvalidation(account *Account, item CodexTicketAttempt) *CodexTicketInvalidation {
	if item.TicketCapturedAt == nil {
		return nil
	}
	encoded, err := json.Marshal(account.Extra[openAICodexTicketExtraKey(item.Model)])
	if err != nil {
		return nil
	}
	var ticket openAICodexTicket
	if json.Unmarshal(encoded, &ticket) != nil || !ticket.Revoked || ticket.Invalidation == nil ||
		ticket.AttemptID != item.ID || ticket.Model != item.Model || !ticket.CapturedAt.Equal(*item.TicketCapturedAt) {
		return nil
	}
	return matchingCodexTicketInvalidation(item, []CodexTicketInvalidation{*ticket.Invalidation})
}

func codexTicketHistoryReason(item CodexTicketAttempt) string {
	if item.Invalidation != nil && item.Invalidation.Reason != "" {
		return "invalidation:" + item.Invalidation.Reason
	}
	if !item.Success {
		if item.Reason != "" && item.Reason != "verified" {
			return "attempt:" + item.Reason
		}
		return ""
	}
	if item.TicketStatus == "ttl_elapsed" {
		return "invalidation:ttl_expired"
	}
	return ""
}

func codexTicketHistoryFilterOptions(items []CodexTicketAttempt) CodexTicketHistoryFilterOptions {
	outcomes := map[string]bool{}
	models, reasons := map[string]bool{}, map[string]bool{}
	for _, item := range items {
		if validCodexTicketOutcome(item.Outcome) {
			outcomes[item.Outcome] = true
		}
		if model := strings.TrimSpace(item.Model); model != "" && len(model) <= 256 {
			models[model] = true
		}
		if reason := codexTicketHistoryReason(item); reason != "" && validCodexTicketHistoryReason(reason) {
			reasons[reason] = true
		}
	}
	options := CodexTicketHistoryFilterOptions{Models: []string{}, Reasons: []string{}, Outcomes: []string{}}
	for outcome := range outcomes {
		options.Outcomes = append(options.Outcomes, outcome)
	}
	sort.Strings(options.Outcomes)
	for model := range models {
		options.Models = append(options.Models, model)
	}
	for reason := range reasons {
		options.Reasons = append(options.Reasons, reason)
	}
	sort.Strings(options.Models)
	sort.Strings(options.Reasons)
	return options
}
