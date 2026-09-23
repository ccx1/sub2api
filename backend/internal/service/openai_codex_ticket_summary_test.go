package service

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func ticketSummaryFrame(kind, model string) string {
	return "event: " + kind + "\ndata: {\"type\":" + jsonString(kind) + ",\"response\":{\"model\":" + jsonString(model) + "}}\n\n"
}

func TestCodexTicketSummaryModelDeclarationPreservesAcceptance(t *testing.T) {
	for _, tc := range []struct {
		name, first, terminal, reason string
		conflict                      bool
	}{
		{"same", "gpt-6-astra", "gpt-6-astra", "", false},
		{"downgraded", "gpt-6-astra", "gpt-5.6-luna", "model_mismatch", true},
		{"wrong throughout", "gpt-5.6-luna", "gpt-5.6-luna", "model_mismatch", false},
		{"terminal recovered", "gpt-5.6-luna", "gpt-6-astra", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capture := &codexTicketExchangeCapture{exchange: &CodexTicketExchange{}}
			body := ticketSummaryFrame("response.created", tc.first) + ticketSummaryFrame("response.completed", tc.terminal)
			err := readOpenAICodexTicketProbeResponseWithDiagnostic(strings.NewReader(body), "gpt-6-astra", capture)
			if tc.reason == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.reason)
			}
			d := capture.exchange.ModelDeclaration
			require.NotNil(t, d)
			require.Equal(t, tc.first, d.FirstModel)
			require.Equal(t, tc.terminal, d.TerminalModel)
			require.NotNil(t, d.Conflict)
			require.Equal(t, tc.conflict, *d.Conflict)
		})
	}
}

func TestCodexTicketSummaryCannotRelaxTerminalRules(t *testing.T) {
	good := ticketSummaryFrame("response.completed", "gpt-6-astra")
	for _, tc := range []struct{ name, body, reason string }{
		{"accumulated mismatch", ticketSummaryFrame("response.completed", "gpt-5.6-luna") + good, "model_mismatch"},
		{"failed then completed", ticketSummaryFrame("response.failed", "gpt-6-astra") + good, "response_failed"},
		{"incomplete then completed", ticketSummaryFrame("response.incomplete", "gpt-6-astra") + good, "response_failed"},
		{"missing blank line", strings.TrimSuffix(good, "\n"), "response_incomplete"},
		{"invalid json", "data: {\"type\":\"response.completed\",broken}\n\n", "response_incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capture := &codexTicketExchangeCapture{exchange: &CodexTicketExchange{}}
			err := readOpenAICodexTicketProbeResponseWithDiagnostic(strings.NewReader(tc.body), "gpt-6-astra", capture)
			require.ErrorContains(t, err, tc.reason)
			plain := newOpenAICodexTicketResponseObserver("gpt-6-astra")
			plain.Observe([]byte(tc.body))
			plain.Finish()
			require.Nil(t, plain.diagnostics)
			require.ErrorContains(t, codexTicketProbeResponseResult(plain), tc.reason)
		})
	}
}

func TestCodexTicketSummaryFramingAndFullModelComparison(t *testing.T) {
	model := strings.Repeat("中", 60)
	body := ticketSummaryFrame("response.created", model) + ticketSummaryFrame("response.completed", model+"x")
	body = strings.ReplaceAll(body, ",\"response\"", ",\ndata: \"response\"")
	body = strings.ReplaceAll(body, "\n", "\r\n")
	o := newOpenAICodexTicketResponseObserver(model)
	o.diagnostics = &codexTicketResponseDiagnostics{wireStatus: 200}
	for _, b := range []byte(body) {
		o.Observe([]byte{b})
	}
	o.Finish()
	require.ErrorContains(t, codexTicketProbeResponseResult(o), "model_mismatch")
	d := o.diagnostics.declaration
	require.Equal(t, d.FirstModel, d.TerminalModel)
	require.True(t, *d.Conflict)
	require.True(t, d.Truncated)
	require.LessOrEqual(t, len(d.FirstModel), 160)

	capture := &codexTicketExchangeCapture{exchange: &CodexTicketExchange{}}
	require.NoError(t, readOpenAICodexTicketProbeResponseWithDiagnostic(strings.NewReader(`{"object":"response","status":"completed","model":"gpt-6-astra"}`), "gpt-6-astra", capture))
	require.Equal(t, "response.completed", capture.exchange.ModelDeclaration.TerminalEvent)
	require.False(t, *capture.exchange.ModelDeclaration.Conflict)
}

type ticketSummaryReadFailure struct{ io.Reader }

func (r ticketSummaryReadFailure) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		return n, io.ErrUnexpectedEOF
	}
	return n, err
}

func TestCodexTicketSummaryQuotaRetainsWireAndOriginalError(t *testing.T) {
	body := `data: {"type":"response.failed","response":{"model":"gpt-6-astra","error":{"type":"usage_limit_reached","resets_in_seconds":60}}}` + "\n\n"
	for _, broken := range []bool{false, true} {
		capture := &codexTicketExchangeCapture{exchange: &CodexTicketExchange{}}
		var reader io.Reader = strings.NewReader(body)
		if broken {
			reader = ticketSummaryReadFailure{reader}
		}
		err := readOpenAICodexTicketProbeResponseWithDiagnostic(reader, "gpt-6-astra", capture)
		require.Error(t, err)
		d := codexTicketProbeUpstreamError(err)
		require.NotNil(t, d)
		require.Equal(t, 200, d.WireHTTPStatus)
		require.Equal(t, 429, d.EffectiveStatus)
		require.Equal(t, "credential", d.Scope)
		require.NotNil(t, d.RetryAt)
		require.True(t, d.RetryAt.After(time.Now()))
		var rejected *openAICodexTicketProbeRejected
		require.False(t, errors.As(err, &rejected))
		if broken {
			require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		} else {
			require.ErrorContains(t, err, "response_failed")
		}
		svc := &OpenAIGatewayService{}
		svc.coolOpenAICodexTicket(ticketTestAccount(1), "tok", err)
		require.False(t, svc.openAICodexTicketCooling(ticketTestAccount(1), "tok"))
		require.Same(t, d, capture.exchange.UpstreamError)
	}
}

func TestCodexTicketSummaryHistoryKeepsDiagnosticsAfterRawExpiry(t *testing.T) {
	history := CodexTicketHistory{}
	for i := range 12 {
		history.Append(CodexTicketAttempt{StartedAt: time.Now().Add(time.Duration(i) * time.Second), HarvestExchange: &CodexTicketExchange{
			Request: &CodexTicketHTTPMessage{Body: "private"}, Response: &CodexTicketHTTPMessage{Body: "raw"},
			Network: &CodexTicketNetwork{PeerAddr: "127.0.0.1:123"}, ModelDeclaration: &CodexTicketModelDeclaration{FirstModel: "gpt-6-astra"},
			UpstreamError: &CodexTicketUpstreamError{WireHTTPStatus: 200, EffectiveStatus: 429}, Signals: &CodexTicketSignals{PlanType: "team"},
		}})
	}
	encoded, err := json.Marshal(history)
	require.NoError(t, err)
	var decoded CodexTicketHistory
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	old := decoded.Items[11].HarvestExchange
	require.Nil(t, old.Request)
	require.Nil(t, old.Response)
	require.Equal(t, "gpt-6-astra", old.ModelDeclaration.FirstModel)
	require.Equal(t, 200, old.UpstreamError.WireHTTPStatus)
	require.Equal(t, "team", old.Signals.PlanType)
	require.Equal(t, "127.0.0.1:123", old.Network.PeerAddr)
}
