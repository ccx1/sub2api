package service

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func codexTicketHistoryQualityFixture() (*Account, CodexTicketAttempt, CodexTicketInvalidation) {
	now := time.Now().UTC()
	captured, expires := now.Add(-time.Minute), now.Add(time.Hour)
	attempt := CodexTicketAttempt{ID: "quality-attempt", Model: "gpt-6-astra", StartedAt: captured,
		Success: true, Reason: "verified", Outcome: "success", TicketCapturedAt: &captured, TicketExpiresAt: &expires}
	event := CodexTicketInvalidation{AttemptID: attempt.ID, Model: attempt.Model, CapturedAt: captured,
		InvalidatedAt: now, Reason: "model_quality_capability_failed", Source: "model_quality"}
	account := ticketTestAccount(41)
	account.Extra = map[string]any{OpenAICodexTicketInvalidationsKey: []CodexTicketInvalidation{event}}
	return account, attempt, event
}

func TestCodexTicketHistoryQualityFailureReclassifiesCumulativeCountsOnce(t *testing.T) {
	account, attempt, event := codexTicketHistoryQualityFixture()
	history := CodexTicketHistory{}
	history.Append(attempt)
	for round := 0; round < 2; round++ {
		changed, err := history.ReconcileQualityFailure(account, attempt.ID, attempt.Model, *attempt.TicketCapturedAt, event.Reason)
		require.NoError(t, err)
		require.Equal(t, round == 0, changed)
		require.EqualValues(t, 1, history.Summary.Total)
		require.Zero(t, history.Summary.Success)
		require.EqualValues(t, 1, history.Summary.Failed)
		require.Zero(t, history.Summary.OutcomeCounts["success"])
		require.EqualValues(t, 1, history.Summary.OutcomeCounts["verification_failed"])
		require.False(t, history.Items[0].Success)
		require.Equal(t, "verification_failed", history.Items[0].Outcome)
		require.Equal(t, event.Reason, history.Items[0].Reason)
	}
	account.Extra = map[string]any{OpenAICodexTicketHistoryKey: history}
	got, err := GetCodexTicketHistory(account, 1, 20, CodexTicketHistoryFilter{Result: "failed", TicketStatus: "invalidated",
		Outcome: "verification_failed", Reason: "invalidation:" + event.Reason})
	require.NoError(t, err)
	require.Len(t, got.Items, 1, "persisted invalidation remains filterable after the rolling ledger is cleared")
	require.Equal(t, event.Reason, got.Items[0].Invalidation.Reason)
	history.PruneCodexTicketHistory(time.Now().Add(OpenAICodexTicketHistoryRetention + time.Hour))
	require.Empty(t, history.Items)
	require.EqualValues(t, 1, history.Summary.Failed, "pruning detail must retain the corrected cumulative count")
}

func TestCodexTicketHistoryQualityFailureBeforeHistoryAppend(t *testing.T) {
	account, attempt, _ := codexTicketHistoryQualityFixture()
	history := CodexTicketHistory{}
	require.NoError(t, history.AppendWithQualityFailures(attempt, account))
	require.EqualValues(t, 1, history.Summary.Total)
	require.Zero(t, history.Summary.Success)
	require.EqualValues(t, 1, history.Summary.Failed)
	require.NotContains(t, history.Summary.OutcomeCounts, "success")
	require.EqualValues(t, 1, history.Summary.OutcomeCounts["verification_failed"])
	require.Equal(t, "invalidated", history.Items[0].TicketStatus)
	require.Empty(t, history.QualityReceipts)
}

func TestCodexTicketHistoryAppendReconcilesFailureBeforeDetailIsPruned(t *testing.T) {
	account, attempt, _ := codexTicketHistoryQualityFixture()
	history := CodexTicketHistory{}
	history.Append(attempt)
	for i := 1; i < OpenAICodexTicketHistoryLimit; i++ {
		history.Append(CodexTicketAttempt{ID: "other", StartedAt: attempt.StartedAt.Add(time.Duration(i) * time.Second), Success: true, Outcome: "success"})
	}
	require.NoError(t, history.AppendWithQualityFailures(CodexTicketAttempt{ID: "new", StartedAt: time.Now(), Success: true, Outcome: "success"}, account))
	require.EqualValues(t, 101, history.Summary.Total)
	require.EqualValues(t, 100, history.Summary.Success)
	require.EqualValues(t, 1, history.Summary.Failed)
	require.EqualValues(t, 100, history.Summary.OutcomeCounts["success"])
	require.EqualValues(t, 1, history.Summary.OutcomeCounts["verification_failed"])
	require.Len(t, history.Items, OpenAICodexTicketHistoryLimit)
}

func TestCodexTicketHistoryQualityFailureRequiresConfirmedReasonAndExactIdentity(t *testing.T) {
	for name, mutate := range map[string]func(*CodexTicketInvalidation){
		"attempt":           func(e *CodexTicketInvalidation) { e.AttemptID = "other" },
		"model":             func(e *CodexTicketInvalidation) { e.Model = "other" },
		"capture":           func(e *CodexTicketInvalidation) { e.CapturedAt = e.CapturedAt.Add(time.Second) },
		"time":              func(e *CodexTicketInvalidation) { e.InvalidatedAt = time.Time{} },
		"earlier time":      func(e *CodexTicketInvalidation) { e.InvalidatedAt = e.CapturedAt.Add(-time.Second) },
		"quarantine error":  func(e *CodexTicketInvalidation) { e.Reason = "model_quality_quarantine_persist_failed" },
		"transport":         func(e *CodexTicketInvalidation) { e.Reason = "model_quality_transport_error" },
		"business mismatch": func(e *CodexTicketInvalidation) { e.Reason = "response_model_mismatch" },
	} {
		t.Run(name, func(t *testing.T) {
			account, attempt, event := codexTicketHistoryQualityFixture()
			mutate(&event)
			account.Extra[OpenAICodexTicketInvalidationsKey] = []CodexTicketInvalidation{event}
			history := CodexTicketHistory{}
			require.NoError(t, history.AppendWithQualityFailures(attempt, account))
			require.True(t, history.Items[0].Success)
			require.EqualValues(t, 1, history.Summary.Success)
			require.Zero(t, history.Summary.Failed)
		})
	}
}

func TestCodexTicketHistoryQualityFailureSupportsModelMismatchAndRevokedTombstone(t *testing.T) {
	account, attempt, event := codexTicketHistoryQualityFixture()
	event.Reason = "model_quality_model_mismatch"
	account.Extra = map[string]any{openAICodexTicketExtraKey(attempt.Model): openAICodexTicket{AttemptID: attempt.ID,
		Model: attempt.Model, CapturedAt: *attempt.TicketCapturedAt, Revoked: true, Invalidation: &event}}
	history := CodexTicketHistory{}
	require.NoError(t, history.AppendWithQualityFailures(attempt, account))
	require.False(t, history.Items[0].Success)
	require.Equal(t, event.Reason, history.Items[0].Reason)
	require.EqualValues(t, 1, history.Summary.Failed)
}

func TestCodexTicketHistoryQualityFailureAfterDetailWasPruned(t *testing.T) {
	for _, limit := range []string{"item count", "retention"} {
		t.Run(limit, func(t *testing.T) {
			account, attempt, event := codexTicketHistoryQualityFixture()
			if limit == "retention" {
				captured := time.Now().Add(-OpenAICodexTicketHistoryRetention - time.Hour).UTC()
				attempt.StartedAt, attempt.TicketCapturedAt, event.CapturedAt = captured, &captured, captured
			}
			account.Extra = map[string]any{openAICodexTicketExtraKey(attempt.Model): openAICodexTicket{
				AttemptID: attempt.ID, Model: attempt.Model, CapturedAt: *attempt.TicketCapturedAt, ExpiresAt: *attempt.TicketExpiresAt}}
			history := CodexTicketHistory{}
			require.NoError(t, history.AppendWithQualityFailures(attempt, account))
			failures := int64(0)
			if limit == "item count" {
				for i := 0; i < OpenAICodexTicketHistoryLimit; i++ {
					require.NoError(t, history.AppendWithQualityFailures(CodexTicketAttempt{ID: fmt.Sprint(i),
						StartedAt: time.Now(), Model: "other-model", Outcome: "failed"}, account))
					failures++
				}
			}
			history.PruneCodexTicketHistory(time.Now())
			for _, item := range history.Items {
				require.NotEqual(t, attempt.ID, item.ID)
			}
			stored, err := DecodeCodexTicketHistory(history)
			require.NoError(t, err)
			account.Extra[OpenAICodexTicketInvalidationsKey] = []CodexTicketInvalidation{event}
			for round := 0; round < 2; round++ {
				changed, err := stored.ReconcileQualityFailure(account, attempt.ID, attempt.Model, *attempt.TicketCapturedAt, event.Reason)
				require.NoError(t, err)
				require.Equal(t, round == 0, changed)
				require.Equal(t, failures+1, stored.Summary.Total)
				require.Zero(t, stored.Summary.Success)
				require.Equal(t, failures+1, stored.Summary.Failed)
				require.Zero(t, stored.Summary.OutcomeCounts["success"])
				require.EqualValues(t, 1, stored.Summary.OutcomeCounts["verification_failed"])
			}
		})
	}
}

func TestCodexTicketHistoryPrunedQualityReceiptRequiresExactIdentity(t *testing.T) {
	for _, field := range []string{"attempt", "model", "capture"} {
		t.Run(field, func(t *testing.T) {
			account, attempt, event := codexTicketHistoryQualityFixture()
			history := CodexTicketHistory{}
			history.Append(attempt)
			history.Items = nil
			switch field {
			case "attempt":
				event.AttemptID = "replacement"
			case "model":
				event.Model = "another-model"
			case "capture":
				event.CapturedAt = event.CapturedAt.Add(time.Second)
			}
			account.Extra[OpenAICodexTicketInvalidationsKey] = []CodexTicketInvalidation{event}
			changed, err := history.ReconcileQualityFailure(account, event.AttemptID, event.Model, event.CapturedAt, event.Reason)
			require.NoError(t, err)
			require.False(t, changed)
			require.EqualValues(t, 1, history.Summary.Success)
			require.Zero(t, history.Summary.Failed)
			require.Len(t, history.QualityReceipts, 1)
		})
	}
}

func TestCodexTicketHistoryQualityReceiptRetentionFollowsCurrentInventory(t *testing.T) {
	account, attempt, _ := codexTicketHistoryQualityFixture()
	account.Extra = map[string]any{}
	history := CodexTicketHistory{}
	for i := 0; i <= OpenAICodexTicketHistoryLimit; i++ {
		attempt.ID = fmt.Sprintf("replacement-%d", i)
		account.Extra[openAICodexTicketExtraKey(attempt.Model)] = openAICodexTicket{AttemptID: attempt.ID,
			Model: attempt.Model, CapturedAt: *attempt.TicketCapturedAt, ExpiresAt: *attempt.TicketExpiresAt}
		require.NoError(t, history.AppendWithQualityFailures(attempt, account))
		require.Len(t, history.QualityReceipts, 1, "receipts are bounded by live inventory, not repeated harvest attempts")
		require.Equal(t, attempt.ID, history.QualityReceipts[0].ID)
	}
	account.Extra[OpenAICodexTicketHistoryKey] = history
	response, err := GetCodexTicketHistory(account, 1, 20)
	require.NoError(t, err)
	payload, err := json.Marshal(response)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "quality_receipts", "internal receipts must not change the public response")
	history.retainCurrentQualityReceipts(account, attempt.TicketExpiresAt.Add(time.Second))
	require.Empty(t, history.QualityReceipts, "expired tickets cannot be checked and must not retain receipts")
	require.EqualValues(t, OpenAICodexTicketHistoryLimit+1, history.Summary.Success)
}

func TestCodexTicketHistoryReconcilesReceiptBeforeExpiryAndReplacementCleanup(t *testing.T) {
	account, attempt, event := codexTicketHistoryQualityFixture()
	expired := time.Now().Add(-time.Second).UTC()
	attempt.TicketExpiresAt, event.InvalidatedAt = &expired, expired.Add(-time.Second)
	history := CodexTicketHistory{}
	history.Append(attempt)
	history.Items = nil
	account.Extra[OpenAICodexTicketInvalidationsKey] = []CodexTicketInvalidation{event}
	require.NoError(t, history.AppendWithQualityFailures(CodexTicketAttempt{ID: "other", StartedAt: time.Now(), Outcome: "failed"}, account))
	require.Zero(t, history.Summary.Success)
	require.EqualValues(t, 2, history.Summary.Failed)
	require.EqualValues(t, 1, history.Summary.OutcomeCounts["verification_failed"])
	require.Empty(t, history.QualityReceipts)
}
