package service

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexTicketHeaderMutatingBody struct {
	io.ReadCloser
	headers http.Header
	state   string
}

func (b *codexTicketHeaderMutatingBody) Read(p []byte) (int, error) {
	b.headers.Set(openAICodexTurnStateHeader, b.state)
	return b.ReadCloser.Read(p)
}

func TestCodexTicketProbeLearnsOnlyOriginalStateAfterMatchingModel(t *testing.T) {
	for _, model := range []string{"gpt-6-astra", "different-model"} {
		t.Run(model, func(t *testing.T) {
			original := fakeCodexTicketState(292)
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				response := codexTicketCompletedResponse(model, original)
				response.Body = &codexTicketHeaderMutatingBody{ReadCloser: response.Body, headers: response.Header, state: "repaired-header"}
				return response, nil
			}})
			state, _, err := svc.fireOpenAICodexTicketProbe(context.Background(), ticketTestAccount(41), "test", "gpt-6-astra", "", time.Second)
			if model != "gpt-6-astra" {
				require.Error(t, err)
				require.Empty(t, state)
				return
			}
			require.NoError(t, err)
			require.Equal(t, original, state)
		})
	}
}
