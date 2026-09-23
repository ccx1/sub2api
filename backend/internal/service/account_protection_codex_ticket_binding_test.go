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

func protectionTicketFixture(t *testing.T) (*AntiDegradeService, *protectionSyncAdminRepo, *OpenAIGatewayService) {
	t.Helper()
	account := ticketTestAccount(1)
	account.Status, account.Concurrency, account.Extra = StatusActive, 4, map[string]any{}
	repo := &protectionSyncAdminRepo{upstreamBillingProbeAccountRepo: &upstreamBillingProbeAccountRepo{
		accounts: map[int64]*Account{1: account},
	}}
	gateway := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	return NewAntiDegradeService(&adminServiceImpl{accountRepo: repo}), repo, gateway
}

func protectionTicket(account *Account) *openAICodexTicket {
	return &openAICodexTicket{AccountID: account.ID, Model: "gpt-6-astra", Verified: true,
		State: fakeCodexTicketState(292), Length: 292, CapturedAt: time.Now().UTC().Add(-time.Minute),
		ExpiresAt: time.Now().UTC().Add(time.Hour), AccountBinding: openAICodexTicketAccountBinding(account),
		Egress: openAICodexTicketEgress(resolveAccountProxyURL(account))}
}

func TestProtectionTransitionsKeepTicketChoiceAndReadiness(t *testing.T) {
	for _, choice := range []string{"enabled", "disabled", "default"} {
		t.Run(choice, func(t *testing.T) {
			service, repo, gateway := protectionTicketFixture(t)
			account := repo.accounts[1]
			if choice != "default" {
				account.Extra[OpenAICodexTicketEnabledExtraKey] = choice == "enabled"
			}
			ticket := protectionTicket(account)
			key := openAICodexTicketExtraKey(ticket.Model)
			account.Extra[key] = ticket
			gateway.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, ticket.Model), ticket)
			for _, enabled := range []bool{true, true, false, false, true} {
				updated, err := service.SetProtection(context.Background(), 1, enabled, !enabled)
				require.NoError(t, err)
				require.Equal(t, enabled, updated.AntiDegradationEnabled())
				require.Equal(t, choice != "disabled", OpenAICodexTicketAccountEnabled(updated))
				if choice == "default" {
					require.NotContains(t, updated.Extra, OpenAICodexTicketEnabledExtraKey)
				}
				persisted := parseOpenAICodexTicketFromAny(1, ticket.Model, updated.Extra[key])
				require.True(t, persisted.accountCompatible(updated))
				require.Equal(t, ticket.State, persisted.State)
				require.True(t, ticket.CapturedAt.Equal(persisted.CapturedAt))
				require.True(t, ticket.ExpiresAt.Equal(persisted.ExpiresAt))
				require.True(t, gateway.lookupOpenAICodexTicket(updated, ticket.Model).usable(time.Now(), updated, gateway.openAICodexTicketConfig()))
				headers := http.Header{}
				require.NoError(t, gateway.applyOpenAICodexTicket(context.Background(), updated, ticket.Model, headers))
				if choice == "disabled" {
					require.Empty(t, headers)
				} else {
					require.Equal(t, ticket.State, headers.Get(openAICodexTurnStateHeader))
					require.True(t, OpenAICodexTicketStatuses(updated, gateway.openAICodexTicketConfig(), time.Now())[0].Ready)
				}
			}
			require.Equal(t, ticket.AccountBinding, openAICodexTicketAccountBinding(account), "不得修改原账号/缓存的共享对象")
		})
	}
}

func TestProtectionTransitionPreservesCookieAndReserveTickets(t *testing.T) {
	service, repo, gateway := protectionTicketFixture(t)
	account := repo.accounts[1]
	gateway.cfg.Gateway.OpenAICodexTicket.CredentialMode = config.CodexTicketCredentialCookie
	ticket := protectionTicket(account)
	ticket.State, ticket.Length, ticket.CredentialMode = "", 0, config.CodexTicketCredentialCookie
	ticket.Cookies = []*http.Cookie{{Name: "session", Value: "test-cookie", Domain: "chatgpt.com", Path: "/", Expires: ticket.ExpiresAt}}
	ticket.Standby = codexTicketLeaf(ticket)
	ticket.Standby.CapturedAt = ticket.CapturedAt.Add(time.Second)
	ticket.Reserve = []*openAICodexTicket{codexTicketLeaf(ticket)}
	ticket.Reserve[0].CapturedAt = ticket.CapturedAt.Add(2 * time.Second)
	account.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
	for _, enabled := range []bool{true, false} {
		updated, err := service.SetProtection(context.Background(), 1, enabled, !enabled)
		require.NoError(t, err)
		inventory := parseOpenAICodexTicketFromAny(1, ticket.Model, updated.Extra[openAICodexTicketExtraKey(ticket.Model)])
		require.Len(t, codexTicketSlots(inventory), 3)
		for index, slot := range codexTicketSlots(inventory) {
			require.True(t, slot.accountCompatible(updated))
			require.Equal(t, ticket.Cookies, slot.Cookies)
			require.True(t, codexTicketSlots(ticket)[index].CapturedAt.Equal(slot.CapturedAt))
		}
		headers := http.Header{}
		require.NoError(t, gateway.applyOpenAICodexTicket(context.Background(), updated, ticket.Model, headers))
		require.Contains(t, headers.Get("Cookie"), "session=test-cookie")
	}
}

func TestProtectionTransitionDoesNotReviveInvalidTickets(t *testing.T) {
	for _, reason := range []string{"revoked", "expired", "binding", "egress"} {
		t.Run(reason, func(t *testing.T) {
			service, repo, gateway := protectionTicketFixture(t)
			account := repo.accounts[1]
			ticket := protectionTicket(account)
			switch reason {
			case "revoked":
				ticket.Revoked = true
			case "expired":
				ticket.ExpiresAt = time.Now().Add(-time.Minute)
			case "binding":
				ticket.AccountBinding = "v2:unrelated-account-configuration"
			case "egress":
				ticket.Egress = openAICodexTicketEgress("http://other.example:8080")
			}
			account.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
			gateway.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, ticket.Model), ticket)
			updated, err := service.SetProtection(context.Background(), 1, true, false)
			require.NoError(t, err)
			require.ErrorIs(t, gateway.applyOpenAICodexTicket(context.Background(), updated, ticket.Model, http.Header{}), ErrOpenAICodexTicketUnavailable)
		})
	}
}

func TestProtectionTransitionFailedWriteKeepsTicketAndConfiguration(t *testing.T) {
	store := newProtectionSettingsAccountStore()
	ticket := protectionTicket(store.account)
	store.account.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
	original, err := json.Marshal(store.account)
	require.NoError(t, err)
	store.writeErr = errors.New("persistence unavailable")
	_, err = NewAntiDegradeService(store).SetProtection(context.Background(), 1, true, false)
	require.ErrorIs(t, err, store.writeErr)
	after, err := json.Marshal(store.account)
	require.NoError(t, err)
	require.JSONEq(t, string(original), string(after))
}

func TestProtectionSwitchDoesNotGateTicketHarvest(t *testing.T) {
	for _, protectionEnabled := range []bool{false, true} {
		for _, ticketEnabled := range []bool{false, true} {
			service, repo, gateway := protectionTicketFixture(t)
			repo.accounts[1].Extra[OpenAICodexTicketEnabledExtraKey] = ticketEnabled
			account, err := service.SetProtection(context.Background(), 1, protectionEnabled, !protectionEnabled)
			require.NoError(t, err)
			gateway.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = "http://harvest.example:8080"
			calls := 0
			gateway.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				calls++
				return codexTicketResponse(), nil
			}}
			gateway.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			if ticketEnabled {
				require.Equal(t, 2, calls, "保护开启和关闭时均正常采集并复验")
				require.True(t, gateway.lookupOpenAICodexTicket(account, "gpt-6-astra").usable(time.Now(), account, gateway.openAICodexTicketConfig()))
			} else {
				require.Zero(t, calls, "保护开启和关闭时均不得启动关闭的打票")
			}
		}
	}
}
