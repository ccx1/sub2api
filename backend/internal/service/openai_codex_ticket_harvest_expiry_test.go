package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketHarvestRespectsCredentialModeExpiry(t *testing.T) {
	for _, mode := range []string{config.CodexTicketCredentialCookie, config.CodexTicketCredentialCookieState} {
		for _, expired := range []bool{false, true} {
			name := mode + "/live_state"
			if expired {
				name = mode + "/expired_state"
			}
			t.Run(name, func(t *testing.T) {
				now := time.Now().UTC().Truncate(time.Second)
				issuedAt := now.Add(-time.Hour + 2*time.Minute)
				if expired {
					issuedAt = now.Add(-2 * time.Hour)
				}
				state, calls := codexTicketStateForExpiryTest(issuedAt, 10), 0
				upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
					calls++
					response := codexTicketCompletedResponse("gpt-6-astra", state)
					if calls == 1 {
						response.Header.Add("Set-Cookie", "session=test; Path=/backend-api; Secure; Max-Age=600")
					}
					return response, nil
				}}
				cfg := config.OpenAICodexTicketConfig{Enabled: true, CredentialMode: mode, TTLSeconds: 240,
					CookieTTLSeconds: 240, HarvestProxyURL: "http://harvest.example:8080", FailClosed: true}
				svc, account := ticketTestService(t, cfg, upstream), ticketTestAccount(41)
				svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
				require.Equal(t, 2, calls)
				ticket := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
				if mode == config.CodexTicketCredentialCookieState && expired {
					require.Nil(t, ticket, "STATE-dependent credentials cannot publish after the protocol deadline")
					return
				}
				require.NotNil(t, ticket)
				require.True(t, ticket.usable(time.Now(), account, svc.openAICodexTicketConfig()))
				if mode == config.CodexTicketCredentialCookie {
					require.Empty(t, ticket.State)
					require.Zero(t, ticket.StateExpiresAt)
					require.True(t, ticket.ExpiresAt.After(now.Add(9*time.Minute)))
					require.True(t, ticket.RevalidateAt.After(now.Add(3*time.Minute)))
					return
				}
				require.True(t, ticket.ExpiresAt.Equal(issuedAt.Add(time.Hour-30*time.Second)))
				require.True(t, ticket.RevalidateAt.Equal(ticket.ExpiresAt))
			})
		}
	}
}
