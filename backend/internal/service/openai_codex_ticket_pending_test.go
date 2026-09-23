package service

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func pendingCodexCookieResponse(values ...string) *http.Response {
	return &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{
		"Set-Cookie": values, "X-Codex-Turn-State": {fakeCodexTicketState(312)},
	}}
}

func TestCodexTicketPendingCookiesMergeWithoutChangingReceipt(t *testing.T) {
	s, account, req := codexCookieObservationFixture(t)
	receipt := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	s.captureCodexTicketCookieCandidate(receipt, req, pendingCodexCookieResponse("ticket_session=updated; Path=/backend-api; Max-Age=120; Secure"))
	first := s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model)
	require.NotNil(t, first)
	require.True(t, first.Candidate.HeaderChanged)
	s.captureCodexTicketCookieCandidate(receipt, req, pendingCodexCookieResponse("csrf=token; Path=/backend-api; Max-Age=120; Secure"))
	merged := s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model)
	require.Greater(t, merged.Candidate.Revision, first.Candidate.Revision)
	require.Len(t, merged.Candidate.Cookies, 2)
	require.Equal(t, "ticket_session=updated", codexTicketCookieHeaderValues(first.Candidate.Jar.Cookies(req.URL)))
	require.Equal(t, "ticket_session=initial-private", req.Header.Get("Cookie"))
	require.Equal(t, "initial-private", receipt.ticket.Cookies[0].Value)
	require.Equal(t, "initial-private", s.lookupOpenAICodexTicket(account, receipt.ticket.Model).Cookies[0].Value)
	merged.Candidate.Jar.SetCookies(req.URL, []*http.Cookie{{Name: "ticket_session", Value: "mutated", Path: "/backend-api"}})
	merged.Ticket.Cookies[0].Value = "mutated"
	latest := s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model)
	require.Equal(t, "ticket_session=updated; csrf=token", codexTicketCookieHeaderValues(latest.Candidate.Jar.Cookies(req.URL)))
	require.Equal(t, "initial-private", latest.Ticket.Cookies[0].Value)
}

func TestCodexTicketPendingCookiesCompareDeletePreservesNewerResponse(t *testing.T) {
	s, account, req := codexCookieObservationFixture(t)
	receipt := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	response := pendingCodexCookieResponse("ticket_session=initial-private; Path=/backend-api; Max-Age=120; Secure")
	s.captureCodexTicketCookieCandidate(receipt, req, response)
	first := s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model)
	require.NotNil(t, first)
	require.False(t, first.Candidate.HeaderChanged, "同值续期必须入队复验，但不应被当作新请求头")
	s.captureCodexTicketCookieCandidate(receipt, req, response)
	require.False(t, s.clearPendingCodexTicketCookies(account.ID, receipt.ticket.Model, first))
	latest := s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model)
	require.NotNil(t, latest)
	require.True(t, s.clearPendingCodexTicketCookies(account.ID, receipt.ticket.Model, latest))
	require.Nil(t, s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model))
}

func TestCodexTicketPendingCookiesRejectsLateOldReceipt(t *testing.T) {
	s, account, req := codexCookieObservationFixture(t)
	old := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	next := codexTicketLeaf(&old.ticket)
	next.CapturedAt = old.ticket.CapturedAt.Add(time.Nanosecond)
	next.Cookies[0].Value = "new-published"
	inventory := codexTicketLeaf(next)
	inventory.Standby = codexTicketLeaf(&old.ticket)
	s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, next.Model), inventory)
	newReq, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	next.applyHeaders(newReq.Header)
	current := &openAICodexTicketReceipt{account: account, ticket: *next, config: old.config}
	s.captureCodexTicketCookieCandidate(current, newReq, pendingCodexCookieResponse("ticket_session=new-candidate; Path=/backend-api; Secure"))
	s.captureCodexTicketCookieCandidate(old, req, pendingCodexCookieResponse("ticket_session=late-old; Path=/backend-api; Secure"))
	pending := s.pendingCodexTicketCookies(account.ID, next.Model)
	require.NotNil(t, pending)
	require.True(t, sameCodexTicket(next, pending.Ticket))
	require.Equal(t, "ticket_session=new-candidate", codexTicketCookieHeaderValues(pending.Candidate.Jar.Cookies(req.URL)))
}

func TestCodexTicketPendingCookiesRejectsRemovedRevokedOrUnmanagedSnapshot(t *testing.T) {
	for _, failure := range []string{"removed", "revoked", "unmanaged"} {
		t.Run(failure, func(t *testing.T) {
			s, account, req := codexCookieObservationFixture(t)
			receipt := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
			inventory := codexTicketLeaf(&receipt.ticket)
			switch failure {
			case "removed":
				inventory.Cookies[0].Value = "replacement"
			case "revoked":
				inventory.Revoked = true
			case "unmanaged":
				req.Header.Set("Cookie", "client=owned")
			}
			s.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, receipt.ticket.Model), inventory)
			s.captureCodexTicketCookieCandidate(receipt, req, pendingCodexCookieResponse("ticket_session=new; Path=/backend-api; Secure"))
			require.Nil(t, s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model))
		})
	}
}

func TestCodexTicketPendingCookiesBoundsStorageAndPrunesExpired(t *testing.T) {
	s, account, req := codexCookieObservationFixture(t)
	receipt := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	for index := range codexTicketPendingLimit {
		s.openaiCodexTicketPending.Store(fmt.Sprintf("unrelated-%d", index), &codexTicketPendingCookies{ExpiresAt: time.Now().Add(time.Minute)})
	}
	response := pendingCodexCookieResponse("ticket_session=new; Path=/backend-api; Secure")
	s.captureCodexTicketCookieCandidate(receipt, req, response)
	require.Nil(t, s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model))
	s.openaiCodexTicketPending.Store("unrelated-0", &codexTicketPendingCookies{ExpiresAt: time.Now().Add(-time.Second)})
	s.captureCodexTicketCookieCandidate(receipt, req, response)
	require.NotNil(t, s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model))
	require.Equal(t, codexTicketPendingLimit, s.prunePendingCodexTicketCookies(time.Now()))
	key := openAICodexTicketKey(account.ID, receipt.ticket.Model)
	s.openaiCodexTicketPending.Store(key, &codexTicketPendingCookies{ExpiresAt: time.Now().Add(-time.Second)})
	require.Nil(t, s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model))
	_, exists := s.openaiCodexTicketPending.Load(key)
	require.False(t, exists)
}

func TestCodexTicketPendingCookieDeletionStaysUnpublished(t *testing.T) {
	s, account, req := codexCookieObservationFixture(t)
	receipt := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
	s.captureCodexTicketCookieCandidate(receipt, req, pendingCodexCookieResponse("ticket_session=; Path=/backend-api; Max-Age=-1; Secure"))
	pending := s.pendingCodexTicketCookies(account.ID, receipt.ticket.Model)
	require.NotNil(t, pending)
	require.True(t, pending.Candidate.HeaderChanged)
	require.Empty(t, pending.Candidate.Cookies)
	require.Equal(t, "initial-private", s.lookupOpenAICodexTicket(account, receipt.ticket.Model).Cookies[0].Value)
}
