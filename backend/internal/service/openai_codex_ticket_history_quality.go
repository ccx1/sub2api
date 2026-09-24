package service

import (
	"encoding/json"
	"time"
)

// 仅保存当前库存中已计成功的票据身份，不含票据、请求或认证内容。
// 独立于明细条数/12h窗口；换票、到期或失败重分类后删除，数量不超过当前有效库存。
type codexTicketQualityReceipt struct {
	ID         string    `json:"id"`
	Model      string    `json:"model"`
	CapturedAt time.Time `json:"captured_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	StartedAt  time.Time `json:"started_at"`
	Outcome    string    `json:"outcome,omitempty"`
}

func (r codexTicketQualityReceipt) attempt() CodexTicketAttempt {
	return CodexTicketAttempt{ID: r.ID, Model: r.Model, TicketCapturedAt: &r.CapturedAt,
		TicketExpiresAt: &r.ExpiresAt, StartedAt: r.StartedAt, Success: true, Outcome: r.Outcome}
}

func (r codexTicketQualityReceipt) matches(item CodexTicketAttempt) bool {
	return r.ID == item.ID && r.Model == item.Model && item.TicketCapturedAt != nil && r.CapturedAt.Equal(*item.TicketCapturedAt)
}

func (h *CodexTicketHistory) rememberQualityReceipts(items []CodexTicketAttempt) {
	for _, item := range items {
		if !item.Success || item.ID == "" || len(item.ID) > 128 || item.Model == "" || len(item.Model) > 256 ||
			item.TicketCapturedAt == nil || item.TicketCapturedAt.IsZero() || item.TicketExpiresAt == nil ||
			!item.TicketExpiresAt.After(*item.TicketCapturedAt) {
			continue
		}
		found := false
		for _, receipt := range h.QualityReceipts {
			found = found || receipt.matches(item)
		}
		if !found {
			h.QualityReceipts = append(h.QualityReceipts, codexTicketQualityReceipt{ID: item.ID, Model: item.Model,
				CapturedAt: *item.TicketCapturedAt, ExpiresAt: *item.TicketExpiresAt, StartedAt: item.StartedAt, Outcome: item.Outcome})
		}
	}
}

func (h *CodexTicketHistory) removeQualityReceipt(item CodexTicketAttempt) {
	retained := h.QualityReceipts[:0]
	for _, receipt := range h.QualityReceipts {
		if !receipt.matches(item) {
			retained = append(retained, receipt)
		}
	}
	h.QualityReceipts = retained
}

func (h *CodexTicketHistory) retainCurrentQualityReceipts(account *Account, now time.Time) {
	retained := h.QualityReceipts[:0]
	for _, receipt := range h.QualityReceipts {
		if !now.Before(receipt.ExpiresAt) {
			continue
		}
		encoded, err := json.Marshal(account.Extra[openAICodexTicketExtraKey(receipt.Model)])
		var inventory openAICodexTicket
		if err != nil || json.Unmarshal(encoded, &inventory) != nil {
			continue
		}
		for _, ticket := range codexTicketSlots(&inventory) {
			if !ticket.Revoked && ticket.AttemptID == receipt.ID && ticket.Model == receipt.Model && ticket.CapturedAt.Equal(receipt.CapturedAt) {
				retained = append(retained, receipt)
				break
			}
		}
	}
	h.QualityReceipts = retained
}

func codexTicketConfirmedQualityFailure(reason string) bool {
	return reason == "model_quality_capability_failed" || reason == "model_quality_model_mismatch"
}

func codexTicketHistoryInvalidations(account *Account) ([]CodexTicketInvalidation, error) {
	var events []CodexTicketInvalidation
	if raw := account.Extra[OpenAICodexTicketInvalidationsKey]; raw != nil {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(encoded, &events); err != nil {
			return nil, err
		}
	}
	return events, nil
}

func codexTicketHistoryInvalidation(account *Account, item CodexTicketAttempt, events []CodexTicketInvalidation) *CodexTicketInvalidation {
	if event := matchingCodexTicketInvalidation(item, events); event != nil {
		return event
	}
	if event := codexTicketHistoryTombstoneInvalidation(account, item); event != nil {
		return event
	}
	if item.Invalidation != nil {
		return matchingCodexTicketInvalidation(item, []CodexTicketInvalidation{*item.Invalidation})
	}
	return nil
}

func applyCodexTicketQualityFailure(item *CodexTicketAttempt, event *CodexTicketInvalidation) bool {
	if !item.Success || event == nil || !codexTicketConfirmedQualityFailure(event.Reason) {
		return false
	}
	item.Success, item.Reason, item.Outcome = false, event.Reason, "verification_failed"
	item.TicketStatus, item.Invalidation = "invalidated", event
	return true
}

// 调用者在账号行锁内持久化结果；账本先于采票历史写入时也不能丢失失败计数。
func (h *CodexTicketHistory) AppendWithQualityFailures(attempt CodexTicketAttempt, account *Account) error {
	events, err := codexTicketHistoryInvalidations(account)
	if err != nil {
		return err
	}
	h.reconcileQualityFailures(account, events, nil)
	applyCodexTicketQualityFailure(&attempt, codexTicketHistoryInvalidation(account, attempt, events))
	h.Append(attempt)
	h.PruneCodexTicketHistory(time.Now())
	h.retainCurrentQualityReceipts(account, time.Now())
	return nil
}

// 仅校正已持久化且精确归属这张票的确认质量失败，重复调用不再次扣减累计成功数。
func (h *CodexTicketHistory) ReconcileQualityFailure(account *Account, attemptID, model string, capturedAt time.Time, reason string) (bool, error) {
	events, err := codexTicketHistoryInvalidations(account)
	if err != nil {
		return false, err
	}
	match := func(item CodexTicketAttempt, event *CodexTicketInvalidation) bool {
		return item.ID == attemptID && item.Model == model && item.TicketCapturedAt != nil &&
			item.TicketCapturedAt.Equal(capturedAt) && event != nil && event.Reason == reason
	}
	return h.reconcileQualityFailures(account, events, match), nil
}

func (h *CodexTicketHistory) reconcileQualityFailures(account *Account, events []CodexTicketInvalidation, match func(CodexTicketAttempt, *CodexTicketInvalidation) bool) bool {
	changed := false
	for i := range h.Items {
		item := &h.Items[i]
		event := codexTicketHistoryInvalidation(account, *item, events)
		if match != nil && !match(*item, event) {
			continue
		}
		previousOutcome := item.Outcome
		if !applyCodexTicketQualityFailure(item, event) {
			continue
		}
		h.reclassifyQualitySummary(previousOutcome, item.StartedAt)
		h.removeQualityReceipt(*item)
		changed = true
	}
	retained := h.QualityReceipts[:0]
	for _, receipt := range h.QualityReceipts {
		item := receipt.attempt()
		event := codexTicketHistoryInvalidation(account, item, events)
		if (match == nil || match(item, event)) && applyCodexTicketQualityFailure(&item, event) {
			h.reclassifyQualitySummary(receipt.Outcome, receipt.StartedAt)
			changed = true
			continue
		}
		retained = append(retained, receipt)
	}
	h.QualityReceipts = retained
	return changed
}

func (h *CodexTicketHistory) reclassifyQualitySummary(previousOutcome string, started time.Time) {
	if h.Summary.Success > 0 {
		h.Summary.Success--
	}
	h.Summary.Failed++
	if h.Summary.OutcomeCounts == nil {
		h.Summary.OutcomeCounts = make(map[string]int64)
	}
	if previousOutcome != "" && h.Summary.OutcomeCounts[previousOutcome] > 0 {
		h.Summary.OutcomeCounts[previousOutcome]--
	}
	h.Summary.OutcomeCounts["verification_failed"]++
	if h.Summary.ClassificationStartedAt == nil {
		h.Summary.ClassificationStartedAt = &started
	}
}
