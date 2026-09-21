package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type ticketDiagnosticPayload struct{ t *testing.T }

func (matcher ticketDiagnosticPayload) Match(value driver.Value) bool {
	raw, ok := value.(string)
	if !ok {
		return false
	}
	var history service.CodexTicketHistory
	if json.Unmarshal([]byte(raw), &history) != nil || len(history.Items) != 2 {
		return false
	}
	current, legacy := history.Items[0], history.Items[1]
	require.Equal(matcher.t, "strict", current.LengthMode)
	require.Equal(matcher.t, "ticket_length_mismatch", current.Reason)
	require.Equal(matcher.t, []int{312}, current.RejectedLengths)
	if current.HarvestExchange == nil || current.HarvestExchange.Response == nil {
		return false
	}
	require.Equal(matcher.t, []string{"actual"}, current.HarvestExchange.ReportedModels)
	require.Equal(matcher.t, `{"model":"actual"}`, current.HarvestExchange.Response.Body)
	if current.HarvestTicketLength == nil || current.TargetLength == nil || current.BusinessTicketLength == nil || current.HarvestHTTPStatus == nil {
		return false
	}
	return *current.HarvestTicketLength == 312 && *current.TargetLength == 292 && *current.BusinessTicketLength == 0 &&
		*current.HarvestHTTPStatus == 200 && legacy.HarvestTicketLength == nil && history.Summary.Total == 2
}

func TestCodexTicketHistoryPersistsDiagnosticsAlongsideLegacyRows(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	history := service.CodexTicketHistory{}
	history.Append(service.CodexTicketAttempt{ID: "legacy", StartedAt: time.Unix(1, 0)})
	previous, err := json.Marshal(history)
	require.NoError(t, err)
	length, target, missing, status := 312, 292, 0, 200
	attempt := service.CodexTicketAttempt{ID: "diagnostic", StartedAt: time.Now(), Reason: "ticket_length_mismatch",
		LengthMode: "strict", TargetLength: &target, RejectedLengths: []int{312}, HarvestTicketLength: &length,
		BusinessTicketLength: &missing, HarvestHTTPStatus: &status,
		HarvestExchange: &service.CodexTicketExchange{RequestedModel: "requested", ReportedModels: []string{"actual"},
			Response: &service.CodexTicketHTTPMessage{StatusCode: 200, Body: `{"model":"actual"}`}}}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT extra`).WithArgs(int64(41), service.OpenAICodexTicketHistoryKey).
		WillReturnRows(sqlmock.NewRows([]string{"history"}).AddRow(previous))
	mock.ExpectExec(`UPDATE accounts SET extra`).WithArgs(int64(41), service.OpenAICodexTicketHistoryKey, ticketDiagnosticPayload{t}).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.RecordCodexTicketAttempt(context.Background(), 41, attempt))
	require.NoError(t, mock.ExpectationsWereMet())
}
