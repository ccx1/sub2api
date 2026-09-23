package admin

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGetCodexTicketHistoryPreservesObservedZeroAndUnknown(t *testing.T) {
	length, missing, target, status := 312, 0, 292, 200
	account := ticketHistoryAccount()
	account.Extra = map[string]any{service.OpenAICodexTicketHistoryKey: service.CodexTicketHistory{
		Items: []service.CodexTicketAttempt{
			{ID: "observed", StartedAt: time.Now(), Reason: "ticket_length_mismatch", LengthMode: "strict",
				TargetLength: &target, RejectedLengths: []int{312}, HarvestTicketLength: &length,
				BusinessTicketLength: &missing, HarvestHTTPStatus: &status, BusinessHTTPStatus: &status,
				HarvestExchange: &service.CodexTicketExchange{CaptureMode: "raw", RequestedModel: "requested", ReportedModels: []string{"actual"},
					Response: &service.CodexTicketHTTPMessage{StatusCode: 200, Body: `{"model":"actual"}`}}},
			{ID: "legacy", Reason: "harvest_failed"},
		},
	}}
	result := ticketHistoryRequest(&ticketHistoryAdminStub{account: account}, "/accounts/41/codex-ticket/history")
	require.Equal(t, http.StatusOK, result.Code)
	var envelope struct {
		Data struct {
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data.Items, 2)
	observed, legacy := envelope.Data.Items[0], envelope.Data.Items[1]
	require.Equal(t, "strict", observed["length_mode"])
	require.Equal(t, float64(292), observed["target_length"])
	require.Equal(t, float64(312), observed["harvest_ticket_length"])
	require.Equal(t, float64(0), observed["business_ticket_length"])
	require.Equal(t, float64(200), observed["harvest_http_status"])
	exchange := observed["harvest_exchange"].(map[string]any)
	require.Equal(t, "raw", exchange["capture_mode"])
	require.Equal(t, "requested", exchange["requested_model"])
	require.Equal(t, []any{"actual"}, exchange["reported_models"])
	require.NotContains(t, legacy, "harvest_exchange")
	for _, key := range []string{"length_mode", "target_length", "harvest_ticket_length", "business_ticket_length", "harvest_http_status"} {
		require.NotContains(t, legacy, key)
	}
}
