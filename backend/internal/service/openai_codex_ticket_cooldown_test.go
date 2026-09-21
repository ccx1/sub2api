package service

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketCooldownRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 20, 6, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, retryAfter string
		interval         int
		want             time.Duration
	}{
		{"baseline", "", 6, 5 * time.Minute},
		{"configured interval", "", 600, 10 * time.Minute},
		{"seconds", " 900 ", 6, 15 * time.Minute},
		{"short seconds", "60", 6, 5 * time.Minute},
		{"date", now.Add(time.Hour).Format(http.TimeFormat), 6, time.Hour},
		{"past date", now.Add(-time.Hour).Format(http.TimeFormat), 6, 5 * time.Minute},
		{"invalid", "secret invalid value", 6, 5 * time.Minute},
		{"negative", "-900", 6, 5 * time.Minute},
		{"fraction", "300.5", 6, 5 * time.Minute},
		{"overflow", "18446744073709551615", 6, time.Duration((1<<63-1)/time.Second) * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, now.Add(tc.want), openAICodexTicketCooldownDeadline(now, tc.interval, tc.retryAfter))
		})
	}
}

func TestCodexTicketCooldownOnlyUsesTypedRejections(t *testing.T) {
	for _, status := range []int{400, 401, 403, 429, 500, 503} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			svc := &OpenAIGatewayService{}
			account := ticketTestAccount(41)
			rejection := &openAICodexTicketProbeRejected{Status: status, RetryAfter: "private-header"}
			svc.coolOpenAICodexTicket(account, "private-token", fmt.Errorf("wrapper: %w", rejection))
			require.Equal(t, status == 401 || status == 403 || status == 429, svc.openAICodexTicketCooling(account, "private-token"))
			require.Equal(t, "codex ticket probe rejected by upstream", rejection.Error())
		})
	}
	svc, account := &OpenAIGatewayService{}, ticketTestAccount(41)
	svc.coolOpenAICodexTicket(account, "test", errors.New("HTTP 429"))
	svc.coolOpenAICodexTicket(account, "test", nil)
	var typedNil *openAICodexTicketProbeRejected
	svc.coolOpenAICodexTicket(account, "test", typedNil)
	require.False(t, svc.openAICodexTicketCooling(account, "test"))
}

func TestCodexTicketCooldownSharesModelsAndIsolatesCredentialIdentities(t *testing.T) {
	svc, account := &OpenAIGatewayService{}, ticketTestAccount(41)
	account.Credentials["refresh_token"], account.Credentials["chatgpt_account_id"] = "refresh-one", "chatgpt-one"
	oldKey := openAICodexTicketCooldownKey(account, "access-one")
	svc.coolOpenAICodexTicket(account, "access-one", &openAICodexTicketProbeRejected{Status: 429})
	otherModel := cloneOpenAICodexTicketAccount(account)
	otherModel.Extra[openAICodexTicketExtraKey("other-model")] = map[string]any{"state": "other"}
	require.True(t, svc.openAICodexTicketCooling(otherModel, "access-one"))
	require.False(t, svc.openAICodexTicketCooling(ticketTestAccount(42), "access-one"))
	require.False(t, svc.openAICodexTicketCooling(account, "access-two"))
	for _, field := range []string{"refresh_token", "chatgpt_account_id"} {
		changed := cloneOpenAICodexTicketAccount(account)
		changed.Credentials[field] = "new-value"
		require.False(t, svc.openAICodexTicketCooling(changed, "access-one"))
	}
	svc.coolOpenAICodexTicket(account, "access-two", &openAICodexTicketProbeRejected{Status: 401})
	newKey := openAICodexTicketCooldownKey(account, "access-two")
	newUntil, _ := svc.openaiCodexTicketCooldown.Load(newKey)
	svc.coolOpenAICodexTicket(account, "access-one", &openAICodexTicketProbeRejected{Status: 403, RetryAfter: "3600"})
	gotNewUntil, _ := svc.openaiCodexTicketCooldown.Load(newKey)
	require.Equal(t, newUntil, gotNewUntil, "late old-token rejection must not alter the new identity")
	require.NotEqual(t, oldKey, newKey)
	for _, secret := range []string{"access-one", "refresh-one", "chatgpt-one"} {
		require.NotContains(t, oldKey, secret)
	}
}

func TestCodexTicketCooldownRetainsSavedTicketAndCleansExpiredEntries(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, nil)
	account := ticketTestAccount(41)
	key := openAICodexTicketKey(account.ID, "gpt-6-astra")
	ticket := &openAICodexTicket{AccountID: account.ID, Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292,
		CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
	account.Extra = map[string]any{openAICodexTicketExtraKey(ticket.Model): ticket}
	svc.openaiCodexTickets.Store(key, ticket)
	svc.coolOpenAICodexTicket(account, "token", &openAICodexTicketProbeRejected{Status: 429})
	require.Same(t, ticket, svc.lookupOpenAICodexTicket(account, ticket.Model))
	require.Same(t, ticket, account.Extra[openAICodexTicketExtraKey(ticket.Model)])
	cooldownKey := openAICodexTicketCooldownKey(account, "token")
	expiredKey := openAICodexTicketCooldownKey(ticketTestAccount(42), "old")
	svc.openaiCodexTicketCooldown.Store(cooldownKey, time.Now().Add(-time.Second))
	svc.openaiCodexTicketCooldown.Store(expiredKey, time.Now().Add(-time.Second))
	svc.openaiCodexTicketCooldown.Store(openAICodexTicketCooldownSweepKey, time.Now().Add(-time.Second))
	require.False(t, svc.openAICodexTicketCooling(account, "token"))
	_, remains := svc.openaiCodexTicketCooldown.Load(expiredKey)
	require.False(t, remains)
	require.Same(t, ticket, svc.lookupOpenAICodexTicket(account, ticket.Model))
}

func TestCodexTicketCooldownConcurrentUpdatesKeepLatestDeadline(t *testing.T) {
	svc, account := &OpenAIGatewayService{}, ticketTestAccount(41)
	longest := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	var workers sync.WaitGroup
	for i := 0; i < 64; i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			header := "300"
			if index == 20 {
				header = longest.Format(http.TimeFormat)
			}
			svc.coolOpenAICodexTicket(account, "token", &openAICodexTicketProbeRejected{Status: 429, RetryAfter: header})
		}(i)
	}
	workers.Wait()
	got, ok := svc.openaiCodexTicketCooldown.Load(openAICodexTicketCooldownKey(account, "token"))
	require.True(t, ok)
	require.Equal(t, longest, got)
}

func TestCodexTicketCooldownInvalidInputsAreNoops(t *testing.T) {
	var nilService *OpenAIGatewayService
	nilService.coolOpenAICodexTicket(ticketTestAccount(41), "token", &openAICodexTicketProbeRejected{Status: 401})
	require.False(t, nilService.openAICodexTicketCooling(ticketTestAccount(41), "token"))
	svc := &OpenAIGatewayService{}
	for _, account := range []*Account{nil, ticketTestAccount(0)} {
		svc.coolOpenAICodexTicket(account, "token", &openAICodexTicketProbeRejected{Status: 401})
		require.False(t, svc.openAICodexTicketCooling(account, "token"))
	}
	svc.coolOpenAICodexTicket(ticketTestAccount(41), " \t", &openAICodexTicketProbeRejected{Status: 401})
	require.False(t, svc.openAICodexTicketCooling(ticketTestAccount(41), ""))
	require.False(t, strings.Contains(openAICodexTicketCooldownKey(ticketTestAccount(41), "token"), "token"))
}
