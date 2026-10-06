package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type stateProbeOpenBody struct {
	*strings.Reader
	readPastEnd bool
	closed      bool
}

func (b *stateProbeOpenBody) Read(p []byte) (int, error) {
	if b.Reader.Len() == 0 {
		b.readPastEnd = true
		return 0, errors.New("upstream keeps stream open after terminal event")
	}
	return b.Reader.Read(p)
}

func (b *stateProbeOpenBody) Close() error { b.closed = true; return nil }

func TestStateProbeStopsAtTerminalWithoutEOF(t *testing.T) {
	for _, tc := range []struct {
		name, stream string
		failed       bool
	}{
		{"completed", stateProbeCompletedStream, false},
		{"failed", stateProbeFailedStream, true},
		{"done_without_completion", "data: [DONE]\n\n", true},
		{"invalid_completion", "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"incomplete\"}}\n\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := &stateProbeOpenBody{Reader: strings.NewReader(tc.stream)}
			shot, err := fireOpenAICodexStateShotRequest(t.Context(), http.Header{}, "gpt-6-astra", "", "", func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: body}, nil
			})
			require.NoError(t, err)
			require.Equal(t, tc.failed, shot.streamErr != nil)
			require.False(t, body.readPastEnd, "a terminal SSE event must not wait for connection close")
			require.True(t, body.closed)
			if tc.name == "failed" {
				require.Contains(t, shot.detail, "overloaded")
			}
		})
	}
}

func TestStateProbeStreamRejectsInvalidEvidence(t *testing.T) {
	for name, stream := range map[string]string{
		"missing_completion": "data: {\"type\":\"response.created\"}\n\n",
		"truncated_event":    strings.TrimSuffix(stateProbeCompletedStream, "\n"),
		"prior_failure":      stateProbeFailedStream + stateProbeCompletedStream,
		"oversized_body":     ":" + strings.Repeat("x", openAICodexStateProbeMaxBody) + "\n\n" + stateProbeCompletedStream,
		"invalid_prior_json": "data: {broken}\n\n" + stateProbeCompletedStream,
	} {
		t.Run(name, func(t *testing.T) {
			shot, err := fireOpenAICodexStateShotRequest(t.Context(), http.Header{}, "gpt-6-astra", "", "", func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(stream))}, nil
			})
			require.NoError(t, err)
			require.Error(t, shot.streamErr)
		})
	}
}

func TestStateProbeDoesNotAcceptCompletedBodyAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, err := fireOpenAICodexStateShotRequest(ctx, http.Header{}, "gpt-6-astra", "", "", func(*http.Request) (*http.Response, error) {
		cancel()
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(stateProbeCompletedStream))}, nil
	})
	require.ErrorIs(t, err, context.Canceled)
}
