package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type ticketQualityHistoryPayload struct{ reason string }

func (want ticketQualityHistoryPayload) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	var history service.CodexTicketHistory
	if json.Unmarshal([]byte(raw), &history) != nil || len(history.Items) != 1 {
		return false
	}
	item := history.Items[0]
	return history.Summary.Total == 1 && history.Summary.Success == 0 && history.Summary.Failed == 1 &&
		history.Summary.OutcomeCounts["success"] == 0 && history.Summary.OutcomeCounts["verification_failed"] == 1 &&
		!item.Success && item.Reason == want.reason && item.Outcome == "verification_failed" &&
		item.TicketStatus == "invalidated" && item.Invalidation != nil && item.Invalidation.Reason == want.reason
}

func ticketQualityHistoryFixture(t *testing.T, recorded bool) ([]byte, service.CodexTicketAttempt, service.CodexTicketInvalidation) {
	t.Helper()
	captured := time.Now().Add(-time.Minute).UTC()
	attempt := service.CodexTicketAttempt{ID: "quality-attempt", StartedAt: captured, Model: "gpt-6-astra",
		TicketCapturedAt: &captured, Success: true, Outcome: "success", Reason: "verified"}
	event := service.CodexTicketInvalidation{AttemptID: attempt.ID, Model: attempt.Model, CapturedAt: captured,
		InvalidatedAt: captured.Add(time.Second), Reason: "model_quality_capability_failed", Source: "model_quality"}
	history := service.CodexTicketHistory{}
	if recorded {
		history.Append(attempt)
	}
	extra, err := json.Marshal(map[string]any{service.OpenAICodexTicketHistoryKey: history,
		service.OpenAICodexTicketInvalidationsKey: []service.CodexTicketInvalidation{event}})
	require.NoError(t, err)
	return extra, attempt, event
}

func expectQualityHistoryRead(mock sqlmock.Sqlmock, extra []byte) {
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT extra FROM accounts WHERE id = \$1 AND deleted_at IS NULL FOR NO KEY UPDATE`).
		WithArgs(int64(41)).WillReturnRows(sqlmock.NewRows([]string{"extra"}).AddRow(extra))
}

func TestMarkCodexTicketAttemptFailedPersistsCountsAndInvalidation(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	extra, attempt, event := ticketQualityHistoryFixture(t, true)
	expectQualityHistoryRead(mock, extra)
	mock.ExpectExec(`UPDATE accounts SET extra`).
		WithArgs(int64(41), service.OpenAICodexTicketHistoryKey, ticketQualityHistoryPayload{event.Reason}).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.MarkCodexTicketAttemptFailed(context.Background(), 41, attempt.ID, attempt.Model, *attempt.TicketCapturedAt, event.Reason))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMarkCodexTicketAttemptFailedBeforeHistoryAppendUsesPersistedLedger(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	extra, attempt, event := ticketQualityHistoryFixture(t, false)
	expectQualityHistoryRead(mock, extra)
	mock.ExpectCommit()
	require.NoError(t, repo.MarkCodexTicketAttemptFailed(context.Background(), 41, attempt.ID, attempt.Model, *attempt.TicketCapturedAt, event.Reason))
	expectQualityHistoryRead(mock, extra)
	mock.ExpectExec(`UPDATE accounts SET extra`).
		WithArgs(int64(41), service.OpenAICodexTicketHistoryKey, ticketQualityHistoryPayload{event.Reason}).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.RecordCodexTicketAttempt(context.Background(), 41, attempt))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestMarkCodexTicketAttemptFailedIsIdempotentAndRejectsOtherTicketIdentity(t *testing.T) {
	for _, scenario := range []string{"already failed", "other attempt", "other model", "other capture", "other reason", "no ledger"} {
		t.Run(scenario, func(t *testing.T) {
			repo, mock := newCodexTicketCASRepo(t)
			extra, attempt, event := ticketQualityHistoryFixture(t, true)
			var err error
			reason := event.Reason
			switch scenario {
			case "already failed":
				var account service.Account
				require.NoError(t, json.Unmarshal(extra, &account.Extra))
				history, err := service.DecodeCodexTicketHistory(account.Extra[service.OpenAICodexTicketHistoryKey])
				require.NoError(t, err)
				history.Items[0].Success, history.Items[0].Reason = false, event.Reason
				history.Items[0].Outcome = "verification_failed"
				history.Summary.Success, history.Summary.Failed = 0, 1
				history.Summary.OutcomeCounts = map[string]int64{"verification_failed": 1}
				account.Extra[service.OpenAICodexTicketHistoryKey] = history
				extra, err = json.Marshal(account.Extra)
				require.NoError(t, err)
			case "other attempt":
				attempt.ID = "other"
			case "other model":
				attempt.Model = "other"
			case "other capture":
				capture := attempt.TicketCapturedAt.Add(time.Second)
				attempt.TicketCapturedAt = &capture
			case "other reason":
				event.Reason = "model_quality_model_mismatch"
				reason = "model_quality_capability_failed"
				var extraMap map[string]any
				require.NoError(t, json.Unmarshal(extra, &extraMap))
				extraMap[service.OpenAICodexTicketInvalidationsKey] = []service.CodexTicketInvalidation{event}
				extra, err = json.Marshal(extraMap)
				require.NoError(t, err)
			case "no ledger":
				history := service.CodexTicketHistory{}
				history.Append(attempt)
				extra, err = json.Marshal(map[string]any{service.OpenAICodexTicketHistoryKey: history})
				require.NoError(t, err)
			}
			expectQualityHistoryRead(mock, extra)
			mock.ExpectCommit()
			require.NoError(t, repo.MarkCodexTicketAttemptFailed(context.Background(), 41, attempt.ID, attempt.Model, *attempt.TicketCapturedAt, reason))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMarkCodexTicketAttemptFailedRollsBackOnReadOrWriteError(t *testing.T) {
	for _, scenario := range []string{"read", "malformed", "missing", "write"} {
		t.Run(scenario, func(t *testing.T) {
			repo, mock := newCodexTicketCASRepo(t)
			extra, attempt, event := ticketQualityHistoryFixture(t, true)
			mock.ExpectBegin()
			read := mock.ExpectQuery(`SELECT extra FROM accounts`).WithArgs(int64(41))
			switch scenario {
			case "read":
				read.WillReturnError(errors.New("read failed"))
			case "malformed":
				read.WillReturnRows(sqlmock.NewRows([]string{"extra"}).AddRow([]byte("broken-json")))
			case "missing":
				read.WillReturnRows(sqlmock.NewRows([]string{"extra"}))
			case "write":
				read.WillReturnRows(sqlmock.NewRows([]string{"extra"}).AddRow(extra))
				mock.ExpectExec(`UPDATE accounts SET extra`).WillReturnError(errors.New("write failed"))
			}
			mock.ExpectRollback()
			require.Error(t, repo.MarkCodexTicketAttemptFailed(context.Background(), 41, attempt.ID, attempt.Model, *attempt.TicketCapturedAt, event.Reason))
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestMarkCodexTicketAttemptFailedRejectsUnconfirmedReason(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	for _, reason := range []string{"", "model_quality_quarantine_persist_failed", "model_quality_transport_error", "response_model_mismatch"} {
		require.ErrorContains(t, repo.MarkCodexTicketAttemptFailed(context.Background(), 41, "attempt", "model", time.Now(), reason), "invalid codex ticket attempt failure")
	}
	require.NoError(t, mock.ExpectationsWereMet())
}

type prunedTicketQualityHistoryPayload struct{ saved *service.CodexTicketHistory }

func (want prunedTicketQualityHistoryPayload) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok || json.Unmarshal([]byte(raw), want.saved) != nil {
		return false
	}
	history := want.saved
	return history.Summary.Total == 101 && history.Summary.Success == 0 && history.Summary.Failed == 101 &&
		history.Summary.OutcomeCounts["success"] == 0 && history.Summary.OutcomeCounts["verification_failed"] == 1 &&
		len(history.Items) == 100 && len(history.QualityReceipts) == 0
}

func TestMarkCodexTicketAttemptFailedAfterDetailPruningPersistsCorrectionOnce(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	_, attempt, event := ticketQualityHistoryFixture(t, false)
	expires := time.Now().Add(time.Hour).UTC()
	attempt.TicketExpiresAt = &expires
	account := &service.Account{Extra: map[string]any{"codex_turn_ticket:" + attempt.Model: map[string]any{
		"attempt_id": attempt.ID, "model": attempt.Model, "captured_at": *attempt.TicketCapturedAt, "expires_at": expires}}}
	history := service.CodexTicketHistory{}
	require.NoError(t, history.AppendWithQualityFailures(attempt, account))
	for i := 0; i < service.OpenAICodexTicketHistoryLimit; i++ {
		require.NoError(t, history.AppendWithQualityFailures(service.CodexTicketAttempt{
			ID: fmt.Sprint(i), StartedAt: time.Now(), Model: "other-model", Outcome: "failed"}, account))
	}
	account.Extra[service.OpenAICodexTicketHistoryKey] = history
	account.Extra[service.OpenAICodexTicketInvalidationsKey] = []service.CodexTicketInvalidation{event}
	extra, err := json.Marshal(account.Extra)
	require.NoError(t, err)
	expectQualityHistoryRead(mock, extra)
	var corrected service.CodexTicketHistory
	mock.ExpectExec(`UPDATE accounts SET extra`).
		WithArgs(int64(41), service.OpenAICodexTicketHistoryKey, prunedTicketQualityHistoryPayload{&corrected}).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.MarkCodexTicketAttemptFailed(context.Background(), 41, attempt.ID, attempt.Model, *attempt.TicketCapturedAt, event.Reason))
	account.Extra[service.OpenAICodexTicketHistoryKey] = corrected
	extra, err = json.Marshal(account.Extra)
	require.NoError(t, err)
	expectQualityHistoryRead(mock, extra)
	mock.ExpectCommit()
	require.NoError(t, repo.MarkCodexTicketAttemptFailed(context.Background(), 41, attempt.ID, attempt.Model, *attempt.TicketCapturedAt, event.Reason))
	require.NoError(t, mock.ExpectationsWereMet())
}
