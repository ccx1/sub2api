package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

func enableQualityObservation(t *testing.T, svc *OpenAIGatewayService) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	svc.codexModelQuality.ctx = ctx
	svc.codexModelQuality.anomalies = make(map[string]time.Time)
}

func TestCodexModelQualityHTTPObservationPreservesResponseAndFlagsAnomalies(t *testing.T) {
	for _, test := range []struct {
		name, body string
		status     int
		anomaly    bool
	}{
		{"matching SSE", qualityResponseBody("gpt-6-astra", "ordinary business answer"), http.StatusOK, false},
		{"missing model SSE", qualityResponseBody("", "ordinary business answer"), http.StatusOK, true},
		{"failed SSE", "data: {\"type\":\"response.failed\",\"response\":{\"status\":\"failed\"}}\n\n", http.StatusOK, true},
		{"failed then completed", "data: {\"type\":\"response.failed\"}\n\n" + qualityResponseBody("gpt-6-astra", "answer"), http.StatusOK, true},
		{"missing model JSON", `{"object":"response","status":"completed","output":[]}`, http.StatusOK, true},
		{"matching JSON", `{"object":"response","status":"completed","model":"gpt-6-astra","output":[]}`, http.StatusOK, false},
		{"rate limited", `{"error":{"message":"rate limited"}}`, http.StatusTooManyRequests, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, account, ticket, req := ticketWatchdogFixture(t)
			enableQualityObservation(t, svc)
			response := &http.Response{StatusCode: test.status, Header: http.Header{"X-Test": {"preserved"}},
				Body: io.NopCloser(strings.NewReader(test.body))}
			svc.observeOpenAICodexTicketResponse(req, response)
			actual, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			require.Equal(t, test.body, string(actual))
			require.Equal(t, test.status, response.StatusCode)
			require.Equal(t, "preserved", response.Header.Get("X-Test"))
			requestBody, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			require.Equal(t, "original body", string(requestBody))
			source := svc.codexModelQualitySource(account.ID, ticket.Model, "automatic")
			if test.anomaly {
				require.Equal(t, "anomaly", source)
			} else {
				require.Equal(t, "automatic", source)
			}
			require.True(t, sameCodexTicket(ticket, svc.lookupOpenAICodexTicket(account, ticket.Model)))
			require.False(t, svc.codexTicketRevoked(openAICodexTicketKey(account.ID, ticket.Model), ticket))
		})
	}
}

func TestCodexModelQualityHTTPObservationRequiresReceipt(t *testing.T) {
	svc, account, ticket, req := ticketWatchdogFixture(t)
	enableQualityObservation(t, svc)
	req = req.WithContext(context.Background())
	body := qualityResponseBody("", "answer")
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
	svc.observeOpenAICodexTicketResponse(req, response)
	actual, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, body, string(actual))
	require.Equal(t, "automatic", svc.codexModelQualitySource(account.ID, ticket.Model, "automatic"))
}

func TestCodexModelQualityWSObservationPreservesFramesAndFlagsAnomalies(t *testing.T) {
	for _, test := range []struct {
		name, body string
		anomaly    bool
	}{
		{"matching", `{"type":"response.completed","response":{"status":"completed","model":"gpt-6-astra"}}`, false},
		{"missing model", `{"type":"response.completed","response":{"status":"completed"}}`, true},
		{"failed", `{"type":"response.failed","response":{"status":"failed","error":{"message":"unavailable"}}}`, true},
		{"incomplete", `{"type":"response.incomplete","response":{"status":"incomplete"}}`, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, account, receipt := codexTicketWSFixture(t)
			enableQualityObservation(t, svc)
			inner := &codexTicketWSFrames{frames: make(chan []byte, 1)}
			conn := svc.observeOpenAICodexTicketWSFrames(context.Background(), inner, receipt)
			request := []byte(`{"type":"response.create","model":"gpt-6-astra","input":[]}`)
			require.NoError(t, conn.WriteFrame(context.Background(), coderws.MessageText, request))
			frame := []byte(test.body)
			original := append([]byte(nil), frame...)
			inner.frames <- frame
			kind, actual, err := conn.ReadFrame(context.Background())
			require.NoError(t, err)
			require.Equal(t, coderws.MessageText, kind)
			require.Equal(t, original, actual)
			require.Equal(t, original, frame)
			require.Equal(t, [][]byte{request}, inner.written)
			source := svc.codexModelQualitySource(account.ID, receipt.ticket.Model, "automatic")
			if test.anomaly {
				require.Equal(t, "anomaly", source)
			} else {
				require.Equal(t, "automatic", source)
			}
			require.True(t, sameCodexTicket(&receipt.ticket, svc.lookupOpenAICodexTicket(account, receipt.ticket.Model)))
			require.NoError(t, conn.Close())
		})
	}
}

func TestCodexModelQualityHTTPObservationIgnoresClientEarlyClose(t *testing.T) {
	svc, account, ticket, req := ticketWatchdogFixture(t)
	enableQualityObservation(t, svc)
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(qualityResponseBody("", "answer")))}
	svc.observeOpenAICodexTicketResponse(req, response)
	require.NoError(t, response.Body.Close())
	require.Equal(t, "automatic", svc.codexModelQualitySource(account.ID, ticket.Model, "automatic"))
	require.True(t, sameCodexTicket(ticket, svc.lookupOpenAICodexTicket(account, ticket.Model)))
}

func TestCodexModelQualityHTTPObservationDetectsInterruptedStream(t *testing.T) {
	for _, test := range []struct {
		name        string
		terminal    error
		cancelled   bool
		wantAnomaly bool
	}{
		{"EOF without terminal", io.EOF, false, true},
		{"unexpected EOF", io.ErrUnexpectedEOF, false, true},
		{"cancelled context", io.ErrUnexpectedEOF, true, false},
		{"cancelled context EOF", io.EOF, true, false},
		{"cancelled read", context.Canceled, false, false},
		{"expired read", context.DeadlineExceeded, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, account, ticket, req := ticketWatchdogFixture(t)
			enableQualityObservation(t, svc)
			ctx, cancel := context.WithCancel(req.Context())
			defer cancel()
			req = req.WithContext(ctx)
			body := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial answer\"}\n\n"
			response := &http.Response{StatusCode: http.StatusOK,
				Body: io.NopCloser(io.MultiReader(strings.NewReader(body), iotest.ErrReader(test.terminal)))}
			svc.observeOpenAICodexTicketResponse(req, response)
			if test.cancelled {
				cancel()
			}
			actual, err := io.ReadAll(response.Body)
			if test.terminal == io.EOF {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, test.terminal)
			}
			require.NoError(t, response.Body.Close())
			require.Equal(t, body, string(actual))
			source := svc.codexModelQualitySource(account.ID, ticket.Model, "automatic")
			if test.wantAnomaly {
				require.Equal(t, "anomaly", source)
			} else {
				require.Equal(t, "automatic", source)
			}
			require.True(t, sameCodexTicket(ticket, svc.lookupOpenAICodexTicket(account, ticket.Model)))
		})
	}
}
