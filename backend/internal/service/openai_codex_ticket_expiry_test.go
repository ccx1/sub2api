package service

import (
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func codexTicketStateForExpiryTest(issuedAt time.Time, blocks int) string {
	raw := make([]byte, 57+16*blocks)
	raw[0] = 0x80
	binary.BigEndian.PutUint64(raw[1:9], uint64(issuedAt.Unix()))
	return base64.URLEncoding.EncodeToString(raw)
}

func TestCodexTicketStateMetadataExtractsIssuedAt(t *testing.T) {
	issuedAt := time.Now().Add(-2 * time.Minute).Truncate(time.Second)
	state := codexTicketStateForExpiryTest(issuedAt, 10)

	metadata, err := parseOpenAICodexTicketStateMetadata(state)

	require.NoError(t, err)
	require.Equal(t, 10, metadata.Blocks)
	require.True(t, issuedAt.Equal(metadata.IssuedAt))
}

func TestCodexTicketStateMetadataRejectsMalformedState(t *testing.T) {
	state := codexTicketStateForExpiryTest(time.Now(), 10)
	for name, value := range map[string]string{
		"empty": "", "oversized": strings.Repeat("A", 8193),
		"whitespace":          state[:12] + "\n" + state[12:],
		"interior padding":    state[:12] + "=" + state[13:],
		"missing padding":     strings.TrimSuffix(state, "="),
		"excess padding":      state + "=",
		"wrong version":       "A" + state[1:],
		"no ciphertext":       codexTicketStateForExpiryTest(time.Now(), 0),
		"invalid issued time": codexTicketStateForExpiryTest(time.Unix(0, 0), 10),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseOpenAICodexTicketStateMetadata(value)
			require.Error(t, err)
		})
	}
}

func TestCodexTicketStateMetadataAcceptsUnpaddedState(t *testing.T) {
	state := strings.TrimRight(codexTicketStateForExpiryTest(time.Now(), 10), "=")
	metadata, err := parseOpenAICodexTicketStateMetadata(state)
	require.NoError(t, err)
	require.Equal(t, 10, metadata.Blocks)
}

func TestCodexTicketValidUsesStateExpiryAfterRevalidateAt(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	issuedAt := now.Add(-2 * time.Minute)
	state := codexTicketStateForExpiryTest(issuedAt, 10)
	ticket := &openAICodexTicket{
		State:          state,
		Length:         len(state),
		CapturedAt:     now.Add(-30 * time.Second),
		ExpiresAt:      now.Add(time.Minute),
		StateExpiresAt: now.Add(30 * time.Minute),
		RevalidateAt:   now.Add(-time.Second),
	}

	require.True(t, ticket.valid(now, len(state)), "soft TTL expiry must not invalidate a ticket before its state expiry")
	require.True(t, ticket.needsRefresh(now, 0), "soft TTL expiry must schedule revalidation")
}

func TestCodexTicketValidRejectsStateExpiry(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	state := codexTicketStateForExpiryTest(now.Add(-2*time.Hour), 10)
	ticket := &openAICodexTicket{
		State:          state,
		Length:         len(state),
		CapturedAt:     now.Add(-time.Minute),
		ExpiresAt:      now.Add(time.Hour),
		StateExpiresAt: now.Add(-time.Second),
		RevalidateAt:   now.Add(-time.Minute),
	}

	require.False(t, ticket.valid(now, len(state)))
}

func TestCodexTicketMalformedStateFallsBackToConfiguredExpiry(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	ticket := &openAICodexTicket{
		State:        "gAAAAA" + "B",
		Length:       7,
		CapturedAt:   now.Add(-time.Minute),
		ExpiresAt:    now.Add(time.Minute),
		RevalidateAt: now.Add(-time.Second),
	}

	require.True(t, ticket.valid(now, ticket.Length))
	ticket.ExpiresAt = now.Add(-time.Second)
	require.False(t, ticket.valid(now, ticket.Length))
}

func TestCodexTicketParsedStateUsesProtocolExpiry(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	issuedAt := now.Add(-2 * time.Minute)
	state := codexTicketStateForExpiryTest(issuedAt, 10)
	raw := map[string]any{
		"state":       state,
		"length":      len(state),
		"captured_at": now.Add(-time.Minute),
		"expires_at":  now.Add(10 * time.Second),
	}
	ticket := parseOpenAICodexTicketFromAny(1, "gpt-6-astra", raw)

	require.NotNil(t, ticket)
	require.True(t, ticket.StateExpiresAt.Equal(issuedAt.Add(time.Hour-30*time.Second)))
	require.True(t, ticket.ExpiresAt.Equal(ticket.StateExpiresAt))
	require.True(t, ticket.RevalidateAt.Equal(now.Add(10*time.Second)))
	require.True(t, ticket.valid(now, len(state)))
}

func TestCodexTicketHydrateExpiredProtocolCannotReviveWithLongConfig(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	state := codexTicketStateForExpiryTest(now.Add(-time.Hour), 10)
	ticket := &openAICodexTicket{State: state, Length: len(state), CapturedAt: now, ExpiresAt: now.Add(time.Hour)}
	hydrateCodexTicketStateExpiry(ticket)
	require.True(t, ticket.needsRefresh(now, 0))
	require.False(t, ticket.valid(now, len(state)))
	require.Nil(t, codexTicketRevalidationSnapshot(ticket, nil, config.OpenAICodexTicketConfig{TTLSeconds: 3600}, now))
}

func TestCodexTicketHydratePreservesSoftDeadlineAcrossPool(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	makeTicket := func() *openAICodexTicket {
		return &openAICodexTicket{State: codexTicketStateForExpiryTest(now, 10), ExpiresAt: now.Add(-time.Second)}
	}
	ticket := makeTicket()
	ticket.Standby, ticket.Reserve = makeTicket(), []*openAICodexTicket{nil, makeTicket()}
	existing := now.Add(time.Minute)
	ticket.Reserve[1].RevalidateAt = existing
	hydrateCodexTicketStateExpiry(ticket)
	hydrateCodexTicketStateExpiry(ticket)
	for _, slot := range []*openAICodexTicket{ticket, ticket.Standby, ticket.Reserve[1]} {
		require.True(t, slot.ExpiresAt.Equal(now.Add(time.Hour-30*time.Second)))
		require.True(t, slot.IssuedAt.Equal(now))
	}
	require.True(t, ticket.RevalidateAt.Equal(now.Add(-time.Second)))
	require.True(t, ticket.Standby.RevalidateAt.Equal(ticket.RevalidateAt))
	require.True(t, ticket.Reserve[1].RevalidateAt.Equal(existing))
}

func TestCodexTicketHydrateCookieStateUsesEarliestActualDeadline(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	for name, cookieDeadline := range map[string]time.Time{
		"cookie first": now.Add(2 * time.Minute), "state first": now.Add(2 * time.Hour), "session fallback": {},
	} {
		t.Run(name, func(t *testing.T) {
			soft := now.Add(time.Minute)
			ticket := &openAICodexTicket{CredentialMode: "cookie_state", State: codexTicketStateForExpiryTest(now, 10), ExpiresAt: soft,
				Cookies: []*http.Cookie{{Name: "session", Value: "test", Expires: cookieDeadline}}}
			hydrateCodexTicketStateExpiry(ticket)
			expected := now.Add(time.Hour - 30*time.Second)
			if cookieDeadline.IsZero() {
				expected = soft
			} else if cookieDeadline.Before(expected) {
				expected = cookieDeadline
			}
			require.True(t, ticket.ExpiresAt.Equal(expected))
			require.True(t, ticket.RevalidateAt.Equal(soft))
			require.True(t, codexTicketHardExpiry(ticket).Equal(expected))
		})
	}
}

func TestCodexTicketHydrateCookieOnlyIgnoresStateExpiry(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	ticket := &openAICodexTicket{CredentialMode: "cookie", State: codexTicketStateForExpiryTest(now.Add(-time.Hour), 10),
		IssuedAt: now.Add(-time.Hour), StateExpiresAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Minute),
		Cookies: []*http.Cookie{{Name: "session", Value: "test", Expires: now.Add(time.Hour)}}}
	hydrateCodexTicketStateExpiry(ticket)
	require.Zero(t, ticket.IssuedAt)
	require.Zero(t, ticket.StateExpiresAt)
	require.True(t, ticket.ExpiresAt.Equal(now.Add(time.Minute)))
	require.True(t, codexTicketHardExpiry(ticket).Equal(ticket.ExpiresAt))
}

func TestCodexTicketHydrateMalformedStateKeepsFallback(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	for _, mode := range []string{"state", "cookie_state"} {
		t.Run(mode, func(t *testing.T) {
			ticket := &openAICodexTicket{CredentialMode: mode, State: "gAAAAA_invalid", ExpiresAt: now.Add(time.Minute),
				IssuedAt: now, StateExpiresAt: now.Add(time.Hour)}
			hydrateCodexTicketStateExpiry(ticket)
			require.Zero(t, ticket.IssuedAt)
			require.Zero(t, ticket.StateExpiresAt)
			require.True(t, ticket.ExpiresAt.Equal(now.Add(time.Minute)))
			require.Zero(t, ticket.RevalidateAt, "unchanged fallback expiry does not need a materialized soft deadline")
			require.True(t, codexTicketHardExpiry(ticket).Equal(ticket.ExpiresAt))
		})
	}
}

func TestCodexTicketHydrateLegacyFallbackKeepsSnapshotAndRevalidates(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, mode := range []string{"", config.CodexTicketCredentialCookie, config.CodexTicketCredentialCookieState} {
		t.Run(mode, func(t *testing.T) {
			ticket := &openAICodexTicket{AccountID: 41, Model: "gpt-6-astra", CredentialMode: mode,
				State: fakeCodexTicketState(292), Length: 292, CapturedAt: now.Add(-2 * time.Minute), ExpiresAt: now.Add(time.Minute)}
			if mode != "" {
				ticket.Cookies = []*http.Cookie{{Name: "session", Value: "test", Expires: now.Add(time.Hour)}}
			}
			parsed := parseOpenAICodexTicketFromAny(ticket.AccountID, ticket.Model, ticket)
			require.Equal(t, ticket, parsed, "reading an unchanged legacy fallback must preserve its fields")
			require.False(t, parsed.needsRefresh(now, 0))
			hydrateCodexTicketSoftRevalidate(parsed, config.OpenAICodexTicketConfig{TTLSeconds: 60, CookieTTLSeconds: 60})
			require.True(t, parsed.needsRefresh(now, 0), "the scheduler must still apply configured TTL to legacy snapshots")
			require.True(t, parsed.ExpiresAt.After(now))
			require.Zero(t, ticket.RevalidateAt, "runtime revalidation must not mutate the persisted input")
		})
	}
}

func TestCodexTicketHardExpiryCannotExtendStateOnCookieRefresh(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	ticket := &openAICodexTicket{CredentialMode: "cookie_state", State: codexTicketStateForExpiryTest(now, 10), ExpiresAt: now.Add(4 * time.Minute),
		Cookies: []*http.Cookie{{Name: "session", Value: "test", Expires: now.Add(2 * time.Minute)}}}
	hydrateCodexTicketStateExpiry(ticket)
	require.True(t, codexTicketHardExpiry(ticket).Equal(now.Add(2*time.Minute)))
	issuedAt, stateExpiry := ticket.IssuedAt, ticket.StateExpiresAt
	ticket.Cookies[0].Expires = now.Add(2 * time.Hour)
	hydrateCodexTicketStateExpiry(ticket)
	require.True(t, codexTicketHardExpiry(ticket).Equal(stateExpiry))
	require.True(t, ticket.IssuedAt.Equal(issuedAt))
	require.True(t, ticket.RevalidateAt.Equal(now.Add(4*time.Minute)))
}

func TestCodexTicketHardExpiryHonorsPublishedCookieDeadline(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	ticket := &openAICodexTicket{CredentialMode: "cookie_state", ExpiresAt: now.Add(time.Minute), StateExpiresAt: now.Add(time.Hour),
		Cookies: []*http.Cookie{{Name: "session", Value: "test", Expires: now.Add(2 * time.Minute)}}}
	require.True(t, codexTicketHardExpiry(ticket).Equal(ticket.ExpiresAt))
	ticket.Cookies[0].Expires = now.Add(-time.Second)
	require.True(t, codexTicketHardExpiry(ticket).Before(now))
	ticket.CredentialMode = "cookie"
	ticket.StateExpiresAt = now.Add(-time.Hour)
	require.True(t, codexTicketHardExpiry(ticket).Equal(ticket.Cookies[0].Expires))
	require.Zero(t, codexTicketHardExpiry(nil))
}

func TestCodexTicketHardExpiryUsesEveryCookie(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	ticket := &openAICodexTicket{CredentialMode: "cookie_state", ExpiresAt: now.Add(time.Hour), StateExpiresAt: now.Add(30 * time.Minute),
		Cookies: []*http.Cookie{
			{Name: "first", Value: "test", Expires: now.Add(10 * time.Minute)},
			{Name: "second", Value: "test", Expires: now.Add(time.Minute)},
		}}
	require.True(t, codexTicketHardExpiry(ticket).Equal(now.Add(time.Minute)))
}
