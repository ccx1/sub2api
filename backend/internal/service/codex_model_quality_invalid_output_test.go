package service

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexModelQualityInvalidCapabilityOutputNeverQuarantines(t *testing.T) {
	for name, response := range codexQualityInvalidSchemaResponses(t) {
		for _, invalidRound := range []int{1, 2} {
			t.Run(name+"/round_"+strconv.Itoa(invalidRound), func(t *testing.T) {
				s, repo, job := qualityRuntimeFixture(t)
				calls := 0
				s.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
					calls++
					answer := response
					if calls < invalidRound {
						answer = `{"selected":[],"transformed":[],"checksum":-999,"flags":[],"final":-999}`
					}
					body := qualityResponseBody(job.ticket.Model, answer)
					return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}, nil
				}}
				s.executeCodexModelQuality(context.Background(), job, repo)
				record, err := repo.LoadCodexModelQuality(context.Background(), job.account.ID, job.ticket.Model)
				require.NoError(t, err)
				require.NotNil(t, record)
				require.Equal(t, invalidRound, calls)
				require.Equal(t, "inconclusive", record.Status.Status)
				require.Equal(t, "invalid_capability_output", record.Status.Reason)
				require.Zero(t, repo.writes)
				require.False(t, s.codexTicketRevoked(openAICodexTicketKey(job.account.ID, job.ticket.Model), job.ticket))
			})
		}
	}
}
