package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func runCodexTicketDiagnosticHarvest(t *testing.T, cfg config.OpenAICodexTicketConfig, respond func(int) (*http.Response, error)) CodexTicketAttempt {
	t.Helper()
	cfg.Enabled, cfg.HarvestProxyURL = true, "http://harvest.example:8080"
	calls := 0
	upstream := &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) { calls++; return respond(calls) }}
	svc := ticketTestService(t, cfg, upstream)
	repo := &codexTicketHistoryRepo{}
	svc.accountRepo = repo
	svc.probeOnceOpenAICodexTicket(context.Background(), ticketTestAccount(41), "gpt-6-astra")
	require.EqualValues(t, 1, repo.history.Summary.Total)
	require.Len(t, repo.history.Items, 1)
	return repo.history.Items[0]
}

func TestCodexTicketDiagnosticsExplainsCandidateRejectionAndActualLength(t *testing.T) {
	for _, tc := range []struct {
		name, state, reason string
		target              int
	}{
		{name: "mismatched length", state: fakeCodexTicketState(312), target: 292, reason: "ticket_length_mismatch"},
		{name: "missing header", target: 292, reason: "ticket_missing"},
		{name: "invalid prefix", state: strings.Repeat("x", 292), target: 292, reason: "ticket_format_invalid"},
		{name: "rejected target from legacy config", state: fakeCodexTicketState(312), target: 312, reason: "ticket_length_rejected"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			item := runCodexTicketDiagnosticHarvest(t, config.OpenAICodexTicketConfig{TargetLength: tc.target}, func(int) (*http.Response, error) {
				return codexTicketCompletedResponse("gpt-6-astra", tc.state), nil
			})
			require.False(t, item.Success)
			require.Equal(t, tc.reason, item.Reason)
			require.Equal(t, config.CodexTicketLengthStrict, item.LengthMode)
			require.NotNil(t, item.TargetLength)
			require.Equal(t, tc.target, *item.TargetLength)
			require.Equal(t, []int{312}, item.RejectedLengths)
			require.NotNil(t, item.HarvestTicketLength)
			require.Equal(t, len(tc.state), *item.HarvestTicketLength)
			require.Equal(t, http.StatusOK, *item.HarvestHTTPStatus)
			require.Nil(t, item.BusinessTicketLength)
			require.Nil(t, item.BusinessHTTPStatus)
		})
	}
}

func TestCodexTicketDiagnosticsAutoSuccessAndModelFailureKeepActual312(t *testing.T) {
	for _, matched := range []bool{true, false} {
		item := runCodexTicketDiagnosticHarvest(t, config.OpenAICodexTicketConfig{LengthMode: config.CodexTicketLengthAuto}, func(int) (*http.Response, error) {
			model := "gpt-6-astra"
			if !matched {
				model = "unexpected-private-model"
			}
			return codexTicketCompletedResponse(model, fakeCodexTicketState(312)), nil
		})
		require.Equal(t, matched, item.Success)
		require.Equal(t, config.CodexTicketLengthAuto, item.LengthMode)
		require.Nil(t, item.TargetLength)
		require.Empty(t, item.RejectedLengths)
		require.Equal(t, 312, *item.HarvestTicketLength)
		require.Equal(t, http.StatusOK, *item.HarvestHTTPStatus)
		if matched {
			require.Equal(t, "verified", item.Reason)
			require.Equal(t, 312, *item.BusinessTicketLength)
			require.Equal(t, http.StatusOK, *item.BusinessHTTPStatus)
		} else {
			require.Equal(t, "harvest_model_mismatch", item.Reason)
			require.Nil(t, item.BusinessTicketLength)
		}
		encoded, err := json.Marshal(item)
		require.NoError(t, err)
		require.Contains(t, string(encoded), fakeCodexTicketState(312))
		require.Equal(t, "raw", item.HarvestExchange.CaptureMode)
		if !matched {
			require.Contains(t, string(encoded), "unexpected-private-model")
		}
	}
}

func TestCodexTicketDiagnosticsHTTPRejectionRetainsTicketAndStatus(t *testing.T) {
	for _, phase := range []int{1, 2} {
		item := runCodexTicketDiagnosticHarvest(t, config.OpenAICodexTicketConfig{LengthMode: config.CodexTicketLengthAuto}, func(call int) (*http.Response, error) {
			response := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312))
			if call == phase {
				response.StatusCode = http.StatusForbidden
			}
			return response, nil
		})
		require.False(t, item.Success)
		require.Equal(t, 312, *item.HarvestTicketLength)
		if phase == 1 {
			require.Equal(t, "harvest_http_rejected", item.Reason)
			require.Equal(t, http.StatusForbidden, *item.HarvestHTTPStatus)
			require.Nil(t, item.BusinessTicketLength)
		} else {
			require.Equal(t, "business_http_rejected", item.Reason)
			require.Equal(t, http.StatusOK, *item.HarvestHTTPStatus)
			require.Equal(t, 312, *item.BusinessTicketLength)
			require.Equal(t, http.StatusForbidden, *item.BusinessHTTPStatus)
		}
	}
}

func TestCodexTicketDiagnosticsNoResponseDiffersFromResponseWithoutTicket(t *testing.T) {
	for _, responded := range []bool{false, true} {
		item := runCodexTicketDiagnosticHarvest(t, config.OpenAICodexTicketConfig{LengthMode: config.CodexTicketLengthAuto}, func(int) (*http.Response, error) {
			if !responded {
				return nil, errors.New("connection reset by peer: private transport value")
			}
			return codexTicketCompletedResponse("gpt-6-astra", ""), nil
		})
		if responded {
			require.Equal(t, "ticket_missing", item.Reason)
			require.NotNil(t, item.HarvestTicketLength)
			require.Zero(t, *item.HarvestTicketLength)
			require.Equal(t, http.StatusOK, *item.HarvestHTTPStatus)
		} else {
			require.Equal(t, "harvest_transport_failed", item.Reason)
			require.Nil(t, item.HarvestTicketLength)
			require.Nil(t, item.HarvestHTTPStatus)
		}
	}
}

func TestCodexTicketDiagnosticsBusinessResponseWithoutTicketStillSucceeds(t *testing.T) {
	item := runCodexTicketDiagnosticHarvest(t, config.OpenAICodexTicketConfig{LengthMode: config.CodexTicketLengthAuto}, func(call int) (*http.Response, error) {
		state := fakeCodexTicketState(312)
		if call == 2 {
			state = ""
		}
		return codexTicketCompletedResponse("gpt-6-astra", state), nil
	})
	require.True(t, item.Success)
	require.Equal(t, 312, *item.HarvestTicketLength)
	require.NotNil(t, item.BusinessTicketLength)
	require.Zero(t, *item.BusinessTicketLength)
	require.Equal(t, http.StatusOK, *item.BusinessHTTPStatus)
}

func TestCodexTicketDiagnosticsKeepsSnapshotWhenSettingsChangeMidProbe(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://harvest.example:8080"}, nil)
	repo := &codexTicketHistoryRepo{}
	svc.accountRepo = repo
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		svc.cfg.Gateway.OpenAICodexTicket.LengthMode = config.CodexTicketLengthAuto
		svc.cfg.Gateway.OpenAICodexTicket.RejectedLengths = []int{352}
		return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(292)), nil
	}}
	svc.probeOnceOpenAICodexTicket(context.Background(), ticketTestAccount(41), "gpt-6-astra")
	require.Len(t, repo.history.Items, 1)
	item := repo.history.Items[0]
	require.Equal(t, "controls_changed", item.Reason)
	require.Equal(t, config.CodexTicketLengthStrict, item.LengthMode)
	require.Equal(t, 292, *item.TargetLength)
	require.Equal(t, []int{312}, item.RejectedLengths)
	require.Equal(t, 292, *item.HarvestTicketLength)
	require.Nil(t, item.BusinessTicketLength)
}

func TestCodexTicketDiagnosticsLegacyHistoryOmitsObservationsInsteadOfInventingZero(t *testing.T) {
	history, err := DecodeCodexTicketHistory(map[string]any{"items": []any{map[string]any{"id": "old", "reason": "ticket_not_matched"}}})
	require.NoError(t, err)
	require.Len(t, history.Items, 1)
	item := history.Items[0]
	require.Empty(t, item.LengthMode)
	require.Nil(t, item.TargetLength)
	require.Nil(t, item.HarvestTicketLength)
	require.Nil(t, item.BusinessTicketLength)
	require.Nil(t, item.HarvestHTTPStatus)
	require.Nil(t, item.BusinessHTTPStatus)
}
