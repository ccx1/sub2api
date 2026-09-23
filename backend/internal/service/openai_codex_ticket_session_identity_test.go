package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func codexSessionIdentityFixture(t *testing.T, mode string) (*Account, config.OpenAICodexTicketConfig, *openAICodexTicket) {
	t.Helper()
	account := ticketTestAccount(41)
	account.Extra = map[string]any{ProxyModeExtraKey: ProxyModeRandom}
	cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{
		Enabled: true, CredentialMode: mode, PoolCapacity: 3,
	})
	expires := time.Now().Add(time.Minute)
	ticket := &openAICodexTicket{AccountID: account.ID, Model: "gpt-6-astra", CredentialMode: mode,
		State: fakeCodexTicketState(292), Length: 292, Verified: true,
		CapturedAt: time.Now(), ExpiresAt: expires, SessionID: "session-one", Egress: "egress-one"}
	if mode == config.CodexTicketCredentialCookieState {
		ticket.Cookies = []*http.Cookie{{Name: "ticket_session", Value: "same-cookie", Path: "/backend-api", Secure: true, Expires: expires}}
	}
	require.True(t, ticket.usable(time.Now(), account, cfg))
	return account, cfg, ticket
}

func TestCodexTicketSessionIdentityPreservesLegacyEncoding(t *testing.T) {
	legacy := &openAICodexTicket{State: "legacy-state", Egress: "legacy-egress"}
	require.Equal(t, "legacy-state", legacy.credentialIdentity())
	legacy.CredentialMode = config.CodexTicketCredentialCookieState
	require.Equal(t, "90230104e0bb904cc563a4b2df477035155e29a39a13389f00130bedb983d475", legacy.credentialIdentity())
	legacy.Egress = "other-egress"
	require.Equal(t, "90230104e0bb904cc563a4b2df477035155e29a39a13389f00130bedb983d475", legacy.credentialIdentity())
}

func TestCodexTicketSessionIdentitySeparatesInventoryAndRevocation(t *testing.T) {
	for _, mode := range []string{config.CodexTicketCredentialState, config.CodexTicketCredentialCookieState} {
		for _, changed := range []string{"session", "egress"} {
			t.Run(mode+"/"+changed, func(t *testing.T) {
				account, cfg, first := codexSessionIdentityFixture(t, mode)
				next := codexTicketLeaf(first)
				if changed == "session" {
					next.SessionID = "session-two"
				} else {
					next.Egress = "egress-two"
				}
				inventory := mergeCodexTicketPublication(first, next, account, cfg)
				require.Len(t, usableCodexTicketPool(inventory, account, cfg, time.Now()), 2)
				require.False(t, sameCodexTicket(first, next))
				require.NotEqual(t, codexTicketExactRevocationKey(first), codexTicketExactRevocationKey(next))
				inventory = mergeCodexTicketPublication(inventory, codexTicketLeaf(next), account, cfg)
				require.Len(t, codexTicketSlots(inventory), 2, "相同完整快照仍应去重")
				remaining := usableCodexTicketPool(revokeCodexTicketSlot(inventory, first), account, cfg, time.Now())
				require.Len(t, remaining, 1)
				require.Equal(t, next.credentialIdentity(), remaining[0].credentialIdentity())
			})
		}
	}
}
