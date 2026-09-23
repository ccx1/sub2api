package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func codexRevalidationFixture(t *testing.T, mode string) (*OpenAIGatewayService, *Account, *openAICodexTicket) {
	t.Helper()
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, CredentialMode: mode,
		Models: []string{"gpt-6-astra"}, TTLSeconds: 240, CookieTTLSeconds: 240,
		HarvestProxyURL: "http://harvest.example:8080", PoolCapacity: 1}, nil)
	account, now := ticketTestAccount(41), time.Now()
	state := codexTicketStateForExpiryTest(now.Add(-5*time.Minute), 10)
	ticket := &openAICodexTicket{AccountID: account.ID, Model: "gpt-6-astra", State: state, Length: len(state),
		CredentialMode: mode, CapturedAt: now.Add(-5 * time.Minute), RevalidateAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(time.Minute), Verified: true, SessionID: "original-session"}
	if mode != "" {
		ticket.Cookies = []*http.Cookie{{Name: "session", Value: "original", Path: "/", Secure: true, Expires: now.Add(20 * time.Minute)}}
	}
	hydrateCodexTicketStateExpiry(ticket)
	ticket.Binding = svc.codexTicketBinding(account)
	ticket.AccountBinding = openAICodexTicketAccountBinding(account)
	ticket.Egress = openAICodexTicketEgress("")
	account.Extra = map[string]any{openAICodexTicketExtraKey(ticket.Model): cloneCodexTicketInventory(ticket)}
	svc.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, ticket.Model), cloneCodexTicketInventory(ticket))
	return svc, account, ticket
}

func TestCodexTicketRevalidationContinuesOriginalStateAfterSoftTTL(t *testing.T) {
	for _, mode := range []string{"", config.CodexTicketCredentialCookieState} {
		t.Run(mode, func(t *testing.T) {
			svc, account, old := codexRevalidationFixture(t, mode)
			calls := 0
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, old.State, req.Header.Get(openAICodexTurnStateHeader))
				require.Equal(t, old.SessionID, req.Header.Get("session_id"))
				require.True(t, sameCodexTicket(old, svc.lookupOpenAICodexTicket(account, old.Model)))
				return codexTicketCompletedResponse(old.Model, ""), nil
			}}
			svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
			next := svc.lookupOpenAICodexTicket(account, old.Model)
			require.Equal(t, 1, calls, "only the original credential needs a business probe")
			require.Equal(t, old.State, next.State)
			require.Equal(t, old.StateExpiresAt, next.StateExpiresAt)
			require.True(t, next.RevalidateAt.After(time.Now()))
			require.True(t, next.CapturedAt.After(old.CapturedAt))
			require.Nil(t, next.Standby)
		})
	}
}

func TestCodexTicketRevalidationFailureKeepsPublishedSnapshot(t *testing.T) {
	for _, failure := range []string{"transport", "persistence"} {
		t.Run(failure, func(t *testing.T) {
			svc, account, old := codexRevalidationFixture(t, config.CodexTicketCredentialCookieState)
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				if failure == "transport" {
					return nil, errors.New("unexpected EOF")
				}
				return codexTicketCompletedResponse(old.Model, ""), nil
			}}
			if failure == "persistence" {
				svc.accountRepo = &codexTicketLifecycleRepo{persist: func(context.Context) error { return errors.New("write failed") }}
			}
			svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
			require.True(t, sameCodexTicket(old, svc.lookupOpenAICodexTicket(account, old.Model)))
		})
	}
}

func TestCodexTicketRevalidationCandidatePublishesOnlyAfterProbe(t *testing.T) {
	svc, account, old := codexRevalidationFixture(t, config.CodexTicketCredentialCookieState)
	req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	require.NoError(t, svc.applyOpenAICodexTicketRequest(account, old.Model, req))
	receipt := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	svc.observeOpenAICodexTicketResponse(req, &http.Response{StatusCode: 429,
		Header: http.Header{"Set-Cookie": {"session=updated; Max-Age=600; Path=/; Secure"}}})
	calls := 0
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(probe *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "session=updated", probe.Header.Get("Cookie"))
		require.Equal(t, "original", svc.lookupOpenAICodexTicket(account, old.Model).Cookies[0].Value)
		return codexTicketCompletedResponse(old.Model, fakeCodexTicketState(312)), nil
	}}
	svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
	require.Equal(t, 1, calls)
	next := svc.lookupOpenAICodexTicket(account, old.Model)
	require.Equal(t, "updated", next.Cookies[0].Value)
	require.Equal(t, old.State, next.State)
	require.Nil(t, svc.pendingCodexTicketCookies(account.ID, old.Model))
	require.Equal(t, "original", receipt.ticket.Cookies[0].Value)
	svc.invalidateOpenAICodexTicket(context.Background(), account, &receipt.ticket)
	require.True(t, sameCodexTicket(next, svc.lookupOpenAICodexTicket(account, old.Model)), "late old response cannot revoke new publication")
}

func TestCodexTicketRevalidationExtendsOnlyUnknownDeadline(t *testing.T) {
	svc, account, old := codexRevalidationFixture(t, "")
	old.State, old.IssuedAt, old.StateExpiresAt = fakeCodexTicketState(292), time.Time{}, time.Time{}
	old.ExpiresAt = time.Now().Add(-time.Second)
	svc.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, old.Model), old)
	account.Extra[openAICodexTicketExtraKey(old.Model)] = old
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		require.Equal(t, old.State, req.Header.Get(openAICodexTurnStateHeader))
		return codexTicketCompletedResponse(old.Model, ""), nil
	}}
	svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
	next := svc.lookupOpenAICodexTicket(account, old.Model)
	require.True(t, next.ExpiresAt.After(time.Now()))
	require.True(t, next.StateExpiresAt.IsZero())
	require.Equal(t, old.State, next.State)
}

func TestCodexTicketRevalidationCASUsesOriginalJSON(t *testing.T) {
	svc, account, old := codexRevalidationFixture(t, "")
	raw := map[string]any{"model": old.Model, "state": old.State, "length": old.Length,
		"captured_at": old.CapturedAt, "expires_at": old.RevalidateAt, "verified": true}
	account.Extra[openAICodexTicketExtraKey(old.Model)] = raw
	expected, err := json.Marshal(raw)
	require.NoError(t, err)
	svc.accountRepo = &codexTicketCASStub{update: func(snapshot *Account, model string, value any) (bool, error) {
		actual, err := json.Marshal(snapshot.Extra[openAICodexTicketExtraKey(model)])
		require.NoError(t, err)
		require.JSONEq(t, string(expected), string(actual))
		return true, nil
	}}
	next := codexTicketRevalidationSnapshot(old, nil, svc.openAICodexTicketConfig(), time.Now())
	require.NotNil(t, next)
	require.True(t, svc.replaceRevalidatedCodexTicket(context.Background(), account, old, next, nil))
}

func TestCodexTicketRevalidationNeverExtendsKnownStateExpiry(t *testing.T) {
	svc, _, old := codexRevalidationFixture(t, "")
	old.StateExpiresAt = time.Now().Add(-time.Second)
	require.Nil(t, codexTicketRevalidationSnapshot(old, nil, svc.openAICodexTicketConfig(), time.Now()))
}
