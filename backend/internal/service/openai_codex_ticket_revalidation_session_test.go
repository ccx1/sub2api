package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketRevalidationSessionFallbackRequiresSuccessfulProbe(t *testing.T) {
	svc, account, old := codexRevalidationFixture(t, config.CodexTicketCredentialCookieState)
	old.SessionID = ""
	old.Cookies[0].Expires = time.Now().Add(-time.Second)
	old.CookieSessionKeys = []string{codexTicketCookieKey(*old.Cookies[0])}
	old.ExpiresAt = old.Cookies[0].Expires
	svc.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, old.Model), cloneCodexTicketInventory(old))
	account.Extra[openAICodexTicketExtraKey(old.Model)] = old
	var session string
	calls := 0
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			session = req.Header.Get("session_id")
		}
		require.NotEmpty(t, session)
		require.Equal(t, session, req.Header.Get("session_id"))
		require.Equal(t, "session=original", req.Header.Get("Cookie"))
		response := codexTicketCompletedResponse(old.Model, "")
		response.Header.Add("Set-Cookie", "session=original; Path=/; Secure")
		return response, nil
	}}
	svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
	next := svc.lookupOpenAICodexTicket(account, old.Model)
	require.Equal(t, 1, calls)
	require.Equal(t, session, next.SessionID)
	require.Equal(t, old.State, next.State)
	require.True(t, next.ExpiresAt.After(time.Now()))
	require.Equal(t, old.StateExpiresAt, next.StateExpiresAt)
	require.Equal(t, old.CookieSessionKeys, next.CookieSessionKeys)
}

func TestCodexTicketRevalidationDiscardsRejectedCandidateWithoutRevokingOriginal(t *testing.T) {
	svc, account, old := codexRevalidationFixture(t, config.CodexTicketCredentialCookieState)
	req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	old.applyHeaders(req.Header)
	svc.captureCodexTicketCookieCandidate(&openAICodexTicketReceipt{account: account, ticket: *old, config: svc.openAICodexTicketConfig()}, req,
		&http.Response{Header: http.Header{"Set-Cookie": {"session=bad; Path=/; Secure; Max-Age=600"}}})
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		return codexTicketCompletedResponse("different-model", ""), nil
	}}
	svc.harvestVerifiedOpenAICodexTicket(context.Background(), account, old.Model)
	require.Nil(t, svc.pendingCodexTicketCookies(account.ID, old.Model))
	require.True(t, sameCodexTicket(old, svc.lookupOpenAICodexTicket(account, old.Model)))
}

func TestCodexTicketRevalidationUnknownStateUsesConfiguredFallback(t *testing.T) {
	svc, _, old := codexRevalidationFixture(t, config.CodexTicketCredentialCookieState)
	old.State, old.StateExpiresAt, old.IssuedAt = fakeCodexTicketState(292), time.Time{}, time.Time{}
	jar := newOpenAICodexTicketCookieJarFromSnapshot(old.Cookies, old.CapturedAt, nil)
	now := time.Now()
	cfg := svc.openAICodexTicketConfig()
	next := codexTicketRevalidationSnapshot(old, jar, cfg, now)
	require.NotNil(t, next)
	require.Equal(t, now.Add(time.Duration(cfg.TTLSeconds)*time.Second), next.ExpiresAt)
}

func TestCodexTicketRevalidationHalfOpenUsesFreshHarvest(t *testing.T) {
	svc, account, old := codexRevalidationFixture(t, "")
	cfg := svc.openAICodexTicketConfig()
	ctx := context.WithValue(context.Background(), codexTicketScheduleKey{}, &codexTicketSchedule{reservation: &CodexTicketReservation{HalfOpen: true}})
	require.False(t, svc.revalidateExistingCodexTicket(ctx, &openAICodexTicketProbeInput{Account: account, Model: old.Model, Config: &cfg}))
}
