package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func sharedParticipationTicketFixture(mode string, now time.Time) (*Account, *openAICodexTicket, config.OpenAICodexTicketConfig) {
	account := sharedTicketProgressAccount()
	ticket := &openAICodexTicket{AccountID: account.ID, Model: "gpt-6-astra", CredentialMode: mode,
		State: fakeCodexTicketState(292), Length: 292, Verified: true,
		CapturedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), AccountBinding: openAICodexTicketAccountBinding(account)}
	if mode != config.CodexTicketCredentialState {
		ticket.Cookies = []*http.Cookie{{Name: "session", Value: "private-cookie-value", Path: "/backend-api", Expires: now.Add(time.Minute)}}
	}
	if mode == config.CodexTicketCredentialCookie {
		ticket.State, ticket.Length = "", 0
	}
	account.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
	cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{Enabled: true, CredentialMode: mode, Models: []string{ticket.Model}})
	return account, ticket, cfg
}

func TestSharedPoolParticipationReadinessIgnoresFailClosed(t *testing.T) {
	now := time.Now()
	for _, mode := range []string{"state", "cookie", "cookie_state"} {
		t.Run(mode, func(t *testing.T) {
			account, ticket, cfg := sharedParticipationTicketFixture(mode, now)
			snapshot := NewSharedPoolTicketAccountSnapshot(account, now)
			for _, failClosed := range []bool{false, true} {
				cfg.FailClosed = failClosed
				require.True(t, snapshot.hasReadyModelForParticipation(cfg, now))
				cfg.Enabled = false
				require.False(t, snapshot.hasReadyModelForParticipation(cfg, now))
				cfg.Enabled = true
				cfg.Models = []string{"another-model"}
				require.False(t, snapshot.hasReadyModelForParticipation(cfg, now))
				cfg.Models = []string{ticket.Model}
			}
			delete(account.Extra, openAICodexTicketExtraKey(ticket.Model))
			require.False(t, NewSharedPoolTicketAccountSnapshot(account, now).hasReadyModelForParticipation(cfg, now))
			require.False(t, (*SharedPoolTicketAccountSnapshot)(nil).hasReadyModelForParticipation(cfg, now))
		})
	}
}

func TestSharedPoolParticipationResolvesCredentialOverrideAndVerification(t *testing.T) {
	now := time.Now()
	for _, mode := range []string{"state", "cookie", "cookie_state"} {
		t.Run(mode, func(t *testing.T) {
			account, ticket, cfg := sharedParticipationTicketFixture(mode, now)
			for _, globalMode := range []string{"state", "cookie", "cookie_state"} {
				cfg.CredentialMode = globalMode
				require.Equal(t, globalMode == mode, NewSharedPoolTicketAccountSnapshot(account, now).hasReadyModelForParticipation(cfg, now))
			}
			account.Extra[CodexTicketCredentialPolicyExtraKey] = map[string]any{"mode": mode}
			ticket.Verified, ticket.VerificationSkipped = false, true
			snapshot := NewSharedPoolTicketAccountSnapshot(account, now)
			for _, verify := range []bool{false, true} {
				cfg.VerifyBusiness = &verify
				require.Equal(t, !verify, snapshot.hasReadyModelForParticipation(cfg, now))
			}
		})
	}
}

func TestSharedPoolParticipationUsesStrictAndAutoStateRules(t *testing.T) {
	now := time.Now()
	account, ticket, cfg := sharedParticipationTicketFixture("state", now)
	ticket.Length, ticket.State = 400, fakeCodexTicketState(400)
	snapshot := NewSharedPoolTicketAccountSnapshot(account, now)
	require.False(t, snapshot.hasReadyModelForParticipation(cfg, now))
	cfg.LengthMode = config.CodexTicketLengthAuto
	require.True(t, snapshot.hasReadyModelForParticipation(cfg, now))
	ticket.Verified = false
	require.False(t, NewSharedPoolTicketAccountSnapshot(account, now).hasReadyModelForParticipation(cfg, now))
	ticket.Verified, ticket.State = true, "gAAAAA"+strings.Repeat("!", 394)
	require.False(t, NewSharedPoolTicketAccountSnapshot(account, now).hasReadyModelForParticipation(cfg, now))
	ticket.Length, ticket.State = 292, fakeCodexTicketState(292)
	cfg.LengthMode, cfg.RejectedLengths = config.CodexTicketLengthStrict, []int{292}
	require.False(t, NewSharedPoolTicketAccountSnapshot(account, now).hasReadyModelForParticipation(cfg, now))
}

func TestSharedPoolParticipationUsesEveryCredentialInventorySlot(t *testing.T) {
	now := time.Now()
	for _, mode := range []string{"state", "cookie", "cookie_state"} {
		for index := 0; index < 4; index++ {
			t.Run(fmt.Sprintf("%s_slot_%d", mode, index), func(t *testing.T) {
				account, primary, cfg := sharedParticipationTicketFixture(mode, now)
				primary.Standby = codexTicketLeaf(primary)
				primary.Reserve = []*openAICodexTicket{codexTicketLeaf(primary), codexTicketLeaf(primary)}
				for i, ticket := range codexTicketSlots(primary) {
					ticket.Revoked = i != index
				}
				snapshot := NewSharedPoolTicketAccountSnapshot(account, now)
				require.True(t, snapshot.hasReadyModelForParticipation(cfg, now))
				selected := codexTicketSlots(primary)[index]
				require.False(t, snapshot.hasReadyModelForParticipation(cfg, selected.hardExpiresAt()))
				selected.Revoked = true
				require.False(t, NewSharedPoolTicketAccountSnapshot(account, now).hasReadyModelForParticipation(cfg, now))
			})
		}
	}
}

func TestSharedPoolParticipationRejectsInvalidCredentialMetadata(t *testing.T) {
	now := time.Now()
	changes := map[string]func(*openAICodexTicket){
		"revoked": func(ticket *openAICodexTicket) { ticket.Revoked = true },
		"expired": func(ticket *openAICodexTicket) { ticket.ExpiresAt = now },
		"binding": func(ticket *openAICodexTicket) { ticket.AccountBinding = "wrong-binding" },
	}
	for _, mode := range []string{"state", "cookie", "cookie_state"} {
		for name, change := range changes {
			t.Run(mode+"_"+name, func(t *testing.T) {
				account, ticket, cfg := sharedParticipationTicketFixture(mode, now)
				change(ticket)
				require.False(t, NewSharedPoolTicketAccountSnapshot(account, now).hasReadyModelForParticipation(cfg, now))
			})
		}
	}
	cookieChanges := map[string]func(*openAICodexTicket){
		"missing":          func(ticket *openAICodexTicket) { ticket.Cookies = nil },
		"expired":          func(ticket *openAICodexTicket) { ticket.Cookies[0].Expires = now },
		"invalid":          func(ticket *openAICodexTicket) { ticket.Cookies[0].Name = "bad name" },
		"unrelated_domain": func(ticket *openAICodexTicket) { ticket.Cookies[0].Domain = "other.example" },
		"unrelated_path":   func(ticket *openAICodexTicket) { ticket.Cookies[0].Path = "/other" },
		"unverified":       func(ticket *openAICodexTicket) { ticket.Verified = false },
	}
	for _, mode := range []string{"cookie", "cookie_state"} {
		for name, change := range cookieChanges {
			t.Run(mode+"_"+name, func(t *testing.T) {
				account, ticket, cfg := sharedParticipationTicketFixture(mode, now)
				change(ticket)
				require.False(t, NewSharedPoolTicketAccountSnapshot(account, now).hasReadyModelForParticipation(cfg, now))
			})
		}
	}
	account, ticket, cfg := sharedParticipationTicketFixture("cookie_state", now)
	ticket.State = "invalid-state"
	require.False(t, NewSharedPoolTicketAccountSnapshot(account, now).hasReadyModelForParticipation(cfg, now))
}

func TestSharedPoolParticipationHonorsProtocolDeadlineAndKeepsSnapshotPrivate(t *testing.T) {
	now := time.Now()
	account, ticket, cfg := sharedParticipationTicketFixture("cookie_state", now)
	ticket.State = codexTicketStateForExpiryTest(now.Add(-30*time.Minute), 10)
	ticket.Length = len(ticket.State)
	ticket.Cookies[0].Expires = now.Add(time.Hour)
	snapshot := NewSharedPoolTicketAccountSnapshot(account, now)
	require.True(t, snapshot.hasReadyModelForParticipation(cfg, now))
	require.False(t, snapshot.hasReadyModelForParticipation(cfg, now.Add(30*time.Minute)))
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	for _, sensitive := range []string{ticket.State, ticket.Cookies[0].Value, "credential-sentinel", "access_token"} {
		require.NotContains(t, string(encoded), sensitive)
		require.NotContains(t, fmt.Sprintf("%#v", snapshot), sensitive)
	}
	ticket.Cookies[0].Expires, ticket.State = now.Add(-time.Hour), "changed"
	require.True(t, snapshot.hasReadyModelForParticipation(cfg, now), "快照不保留可变原票引用")
}
