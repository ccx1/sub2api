package service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketRevalidationRotatedCandidateFailure(t *testing.T) {
	for _, failure := range []string{"model_mismatch", "transport"} {
		t.Run(failure, func(t *testing.T) {
			svc, account, old := codexRevalidationFixture(t, config.CodexTicketCredentialCookieState)
			req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
			require.NoError(t, err)
			old.applyHeaders(req.Header)
			svc.captureCodexTicketCookieCandidate(&openAICodexTicketReceipt{
				account: account, ticket: *old, config: svc.openAICodexTicketConfig(),
			}, req, pendingCodexCookieResponse("session=pending; Path=/; Secure; Max-Age=600"))
			calls := 0
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(probe *http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					require.Equal(t, "session=pending", probe.Header.Get("Cookie"))
					response := codexTicketCompletedResponse(old.Model, "")
					response.Header.Add("Set-Cookie", "session=rotated; Path=/; Secure; Max-Age=600")
					return response, nil
				}
				require.Equal(t, "session=rotated", probe.Header.Get("Cookie"))
				if failure == "transport" {
					return nil, errors.New("unexpected EOF")
				}
				return codexTicketCompletedResponse("different-model", ""), nil
			}}
			svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
			require.Equal(t, 2, calls)
			require.True(t, sameCodexTicket(old, svc.lookupOpenAICodexTicket(account, old.Model)))
			if failure == "transport" {
				require.NotNil(t, svc.pendingCodexTicketCookies(account.ID, old.Model))
			} else {
				require.Nil(t, svc.pendingCodexTicketCookies(account.ID, old.Model))
			}
		})
	}
}
