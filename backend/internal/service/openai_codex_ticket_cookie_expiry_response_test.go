package service

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketCookieResponseMaxAgeTakesPrecedence(t *testing.T) {
	now := time.Now()
	ticket := &openAICodexTicket{CredentialMode: "cookie", CapturedAt: now, ExpiresAt: now.Add(20 * time.Second),
		Cookies: []*http.Cookie{{Name: "session", Value: "same", Expires: now.Add(20 * time.Second)}}}
	for _, test := range []struct {
		name, attributes string
		changed          bool
	}{
		{"shorter age", "Max-Age=1", true},
		{"shorter age overrides later expiry", "Max-Age=1; Expires=" + now.Add(time.Hour).UTC().Format(http.TimeFormat), true},
		{"longer age overrides expired date", "Max-Age=120; Expires=" + now.Add(-time.Hour).UTC().Format(http.TimeFormat), false},
		{"huge age does not overflow", "Max-Age=9223372036854775807", false},
		{"same value extension", "Max-Age=120", false},
		{"expiry without age", "Expires=" + now.Add(time.Second).UTC().Format(http.TimeFormat), true},
		{"delete", "Max-Age=0", true},
		{"negative delete", "Max-Age=-1", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			headers := http.Header{"Set-Cookie": {"session=same; Path=/backend-api; " + test.attributes}}
			require.Equal(t, test.changed, ticket.cookieResponseChanged(headers))
			require.Equal(t, now.Add(20*time.Second), ticket.ExpiresAt)
			require.Equal(t, now.Add(20*time.Second), ticket.Cookies[0].Expires, "响应不能直接续期已验证快照")
		})
	}
}

func TestCodexTicketCookieWatchdogsKeepReceiptAfterShortenedMaxAge(t *testing.T) {
	for _, transport := range []string{"http", "websocket_handshake"} {
		t.Run(transport, func(t *testing.T) {
			s, _, req := codexCookieObservationFixture(t)
			writes := captureTicketInvalidations(s)
			headers := http.Header{"Set-Cookie": {"ticket_session=initial-private; Path=/backend-api; Max-Age=1; Secure"}}
			if transport == "http" {
				s.observeOpenAICodexTicketResponse(req, &http.Response{StatusCode: http.StatusTooManyRequests, Header: headers})
			} else {
				snapshot := req.Context().Value(openAICodexTicketReceiptKey{}).(*openAICodexTicketReceipt)
				codexTicketWSReceiptFromSnapshot(snapshot).observeHandshake(req.Context(), s, headers)
			}
			require.Empty(t, writes, "Cookie changes wait for scheduled business revalidation")
		})
	}
}
