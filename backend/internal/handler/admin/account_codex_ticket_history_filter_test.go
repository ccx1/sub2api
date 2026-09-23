package admin

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGetCodexTicketHistoryRejectsInvalidFiltersBeforeLoading(t *testing.T) {
	for _, query := range []url.Values{
		{"result": {"bad"}}, {"ticket_status": {"valid"}}, {"model": {strings.Repeat("m", 257)}},
		{"reason": {"harvest_model_mismatch"}}, {"reason": {"attempt:"}},
		{"reason": {"invalidation:" + strings.Repeat("r", 160)}}, {"reason": {"attempt:bad reason"}},
		{"started_from": {"2026-09-21"}}, {"started_to": {"invalid"}},
		{"started_from": {"2026-09-22T00:00:00Z"}, "started_to": {"2026-09-21T00:00:00Z"}},
	} {
		t.Run(query.Encode(), func(t *testing.T) {
			stub := &ticketHistoryAdminStub{account: ticketHistoryAccount()}
			result := ticketHistoryRequest(stub, "/accounts/41/codex-ticket/history?"+query.Encode())
			require.Equal(t, http.StatusBadRequest, result.Code)
			require.Empty(t, stub.calls)
		})
	}
}

func TestGetCodexTicketHistoryCombinesFiltersWithLifecycleAndPagination(t *testing.T) {
	base := time.Date(2026, 9, 21, 1, 0, 0, 0, time.UTC)
	expires := base.Add(time.Hour)
	history := service.CodexTicketHistory{}
	for _, item := range []service.CodexTicketAttempt{
		{ID: "failed", Model: "gpt-6-astra", StartedAt: base, Reason: "harvest_model_mismatch"},
		{ID: "first", Model: "gpt-6-astra", StartedAt: base.Add(time.Minute), Success: true, Reason: "verified", TicketCapturedAt: &base, TicketExpiresAt: &expires},
		{ID: "second", Model: "gpt-6-astra", StartedAt: base.Add(2 * time.Minute), Success: true, Reason: "verified", TicketCapturedAt: &base, TicketExpiresAt: &expires},
		{ID: "other", Model: "gpt-5.6-sol", StartedAt: base.Add(3 * time.Minute), Success: true, Reason: "verified"},
	} {
		history.Append(item)
	}
	account := ticketHistoryAccount()
	account.Extra = map[string]any{
		service.OpenAICodexTicketHistoryKey: history,
		service.OpenAICodexTicketInvalidationsKey: []service.CodexTicketInvalidation{
			{AttemptID: "first", Model: "gpt-6-astra", CapturedAt: base, InvalidatedAt: base.Add(8 * time.Minute), Reason: "response_model_mismatch", Source: "http"},
			{AttemptID: "second", Model: "gpt-6-astra", CapturedAt: base, InvalidatedAt: base.Add(9 * time.Minute), Reason: "response_model_mismatch", Source: "websocket"},
		},
	}
	query := url.Values{"page": {"2"}, "page_size": {"1"}, "result": {"success"}, "ticket_status": {"invalidated"},
		"model": {" gpt-6-astra "}, "reason": {"invalidation:response_model_mismatch"},
		"started_from": {base.Add(time.Minute).Format(time.RFC3339)}, "started_to": {base.Add(2 * time.Minute).Format(time.RFC3339)}}
	result := ticketHistoryRequest(&ticketHistoryAdminStub{account: account}, "/accounts/41/codex-ticket/history?"+query.Encode())
	require.Equal(t, http.StatusOK, result.Code)
	var envelope struct {
		Data service.CodexTicketHistory `json:"data"`
	}
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &envelope))
	require.Equal(t, 2, envelope.Data.Total)
	require.EqualValues(t, 4, envelope.Data.Summary.Total)
	require.EqualValues(t, 3, envelope.Data.Summary.Success)
	require.Len(t, envelope.Data.Items, 1)
	require.Equal(t, "first", envelope.Data.Items[0].ID)
	require.True(t, envelope.Data.Items[0].Success)
	require.Equal(t, "http", envelope.Data.Items[0].Invalidation.Source)
	require.Equal(t, []string{"gpt-5.6-sol", "gpt-6-astra"}, envelope.Data.FilterOptions.Models)
	require.Equal(t, []string{"attempt:harvest_model_mismatch", "invalidation:response_model_mismatch"}, envelope.Data.FilterOptions.Reasons)
}
