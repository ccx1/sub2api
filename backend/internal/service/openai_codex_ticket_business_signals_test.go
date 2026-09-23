package service

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const ticketBusinessQuotaEvent = `{"type":"codex.rate_limits","plan_type":"pro","metered_limit_name":"premium","rate_limits":{"primary":{"used_percent":25,"window_minutes":300,"reset_at":1790000000}},"additional_rate_limits":{"spark":{"primary":{"used_percent":40,"window_minutes":300}}},"credential":"must-not-persist"}`
const ticketBusinessMetadataEvent = `{"type":"codex.response.metadata","headers":{"X-Codex-Safety-Buffering-Enabled":"true","X-Codex-Safety-Buffering-Faster-Model":"gpt-5.6-luna","X-Codex-Turn-State":"must-not-persist","Cookie":"must-not-persist"}}`

func TestCodexTicketBusinessHTTPSignalsReachExistingInvalidation(t *testing.T) {
	s, _, _, request := ticketWatchdogFixture(t)
	writes := captureTicketInvalidations(s)
	body := "data: " + ticketBusinessQuotaEvent + "\n\ndata: " + ticketBusinessMetadataEvent + "\n\n" + ticketSummaryFrame("response.completed", "gpt-other")
	response := codexTicketCompletedResponse("gpt-other", "")
	response.Body = io.NopCloser(strings.NewReader(body))
	s.observeOpenAICodexTicketResponse(request, response)
	forwarded, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, body, string(forwarded))
	signals := awaitTicketInvalidation(t, writes).Invalidation.Signals
	assertTicketBusinessSignals(t, signals)
	require.Empty(t, writes)
}

func TestCodexTicketBusinessWSSignalsReachExistingInvalidation(t *testing.T) {
	s, _, receipt := codexTicketWSFixture(t)
	writes := captureTicketInvalidations(s)
	watchdog := receipt.watch(context.Background(), s, receipt.ticket.Model)
	quota, metadata := []byte(ticketBusinessQuotaEvent), []byte(ticketBusinessMetadataEvent)
	watchdog.observe(quota)
	watchdog.observe(metadata)
	require.Equal(t, ticketBusinessQuotaEvent, string(quota))
	require.Equal(t, ticketBusinessMetadataEvent, string(metadata))
	require.Empty(t, writes, "quota and metadata must not add persistence calls")
	watchdog.observe([]byte(`{"type":"response.completed","response":{"model":"gpt-other","status":"completed"}}`))
	stored := awaitTicketInvalidation(t, writes)
	assertTicketBusinessSignals(t, stored.Invalidation.Signals)
	watchdog.observe([]byte(`{"type":"codex.rate_limits","plan_type":"changed","rate_limits":{"primary":{"used_percent":99}}}`))
	require.Equal(t, "pro", stored.Invalidation.Signals.PlanType)
	require.Equal(t, float64(25), *stored.Invalidation.Signals.Quota[0].UsedPercent)
	require.Empty(t, writes, "one used ticket only records its first revocation")
}

func TestCodexTicketBusinessSignalsDoNotTurnQuotaIntoRevocation(t *testing.T) {
	s, _, receipt := codexTicketWSFixture(t)
	writes := captureTicketInvalidations(s)
	watchdog := receipt.watch(context.Background(), s, receipt.ticket.Model)
	watchdog.observe([]byte(ticketBusinessQuotaEvent))
	watchdog.observe([]byte(`{"type":"response.failed","response":{"error":{"type":"usage_limit_reached"}}}`))
	require.Empty(t, writes)
	completed, matches := watchdog.observer.Result()
	require.False(t, completed)
	require.False(t, matches)
}

func assertTicketBusinessSignals(t *testing.T, signals *CodexTicketSignals) {
	t.Helper()
	require.NotNil(t, signals)
	require.NotNil(t, signals.SafetyBufferingEnabled)
	require.True(t, *signals.SafetyBufferingEnabled)
	require.Equal(t, "gpt-5.6-luna", signals.FasterModel)
	require.Equal(t, "pro", signals.PlanType)
	require.Equal(t, "premium", signals.ActiveLimit)
	require.Len(t, signals.Quota, 2)
	encoded, err := json.Marshal(signals)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "must-not-persist")
}
