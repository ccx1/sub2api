package service

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexTicketDiagnosticBrokenBody struct{}

func (codexTicketDiagnosticBrokenBody) Read([]byte) (int, error) {
	return 0, errors.New("connection reset by peer with private details")
}

func TestCodexTicketDiagnosticsRetainsReceivedHeadersForAllResponseFailures(t *testing.T) {
	for _, phase := range []int{1, 2} {
		for _, tc := range []struct{ kind, suffix string }{
			{"model_mismatch", "model_mismatch"}, {"missing_body", "response_incomplete"},
			{"truncated", "response_incomplete"}, {"failed", "response_failed"},
			{"oversize", "response_incomplete"}, {"interrupted", "transport_failed"},
		} {
			t.Run(tc.kind+string(rune('0'+phase)), func(t *testing.T) {
				item := runCodexTicketDiagnosticHarvest(t, config.OpenAICodexTicketConfig{LengthMode: config.CodexTicketLengthAuto}, func(call int) (*http.Response, error) {
					response := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312))
					if call != phase {
						return response, nil
					}
					switch tc.kind {
					case "model_mismatch":
						return codexTicketCompletedResponse("wrong-model", fakeCodexTicketState(312)), nil
					case "missing_body":
						response.Body = nil
					case "truncated":
						response.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\"}\n\n"))
					case "failed":
						response.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\",\"error\":{\"message\":\"private error\"}}\n\n"))
					case "oversize":
						response.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", openAICodexTicketProbeResponseLimit+1)))
					case "interrupted":
						response.Body = io.NopCloser(codexTicketDiagnosticBrokenBody{})
					}
					return response, nil
				})
				stage := "harvest"
				if phase == 2 {
					stage = "business"
				}
				require.False(t, item.Success)
				require.Equal(t, stage+"_"+tc.suffix, item.Reason)
				require.Equal(t, 312, *item.HarvestTicketLength)
				require.Equal(t, http.StatusOK, *item.HarvestHTTPStatus)
				if phase == 2 {
					require.Equal(t, 312, *item.BusinessTicketLength)
					require.Equal(t, http.StatusOK, *item.BusinessHTTPStatus)
				} else {
					require.Nil(t, item.BusinessTicketLength)
					require.Nil(t, item.BusinessHTTPStatus)
				}
			})
		}
	}
}

func TestCodexTicketDiagnosticsResponseReturnedWithTransportErrorStillRecordsObservedHeader(t *testing.T) {
	item := runCodexTicketDiagnosticHarvest(t, config.OpenAICodexTicketConfig{LengthMode: config.CodexTicketLengthAuto}, func(int) (*http.Response, error) {
		return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312)), errors.New("connection reset")
	})
	require.Equal(t, "harvest_transport_failed", item.Reason)
	require.Equal(t, 312, *item.HarvestTicketLength)
	require.Equal(t, http.StatusOK, *item.HarvestHTTPStatus)
	require.Nil(t, item.BusinessTicketLength)
}

func TestCodexTicketDiagnosticsBusinessWithoutResponseDoesNotInventAnEmptyTicket(t *testing.T) {
	item := runCodexTicketDiagnosticHarvest(t, config.OpenAICodexTicketConfig{LengthMode: config.CodexTicketLengthAuto}, func(call int) (*http.Response, error) {
		if call == 2 {
			return nil, errors.New("connection reset")
		}
		return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312)), nil
	})
	require.Equal(t, "business_transport_failed", item.Reason)
	require.Equal(t, 312, *item.HarvestTicketLength)
	require.Equal(t, http.StatusOK, *item.HarvestHTTPStatus)
	require.Nil(t, item.BusinessTicketLength)
	require.Nil(t, item.BusinessHTTPStatus)
}
