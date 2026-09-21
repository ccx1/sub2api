package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func codexTicketAutoFixture(t *testing.T, model string, length int) (*OpenAIGatewayService, *accountTicketProxyRepo, *codexTicketVerificationUpstream) {
	t.Helper()
	svc, repo := accountTicketProxyFixture(t, "random")
	svc.cfg.Gateway.OpenAICodexTicket.LengthMode = config.CodexTicketLengthAuto
	svc.cfg.Gateway.OpenAICodexTicket.FailClosed = true
	svc.cfg.Gateway.OpenAICodexTicket.Models = []string{model}
	upstream := &codexTicketVerificationUpstream{respond: func(int) *http.Response {
		return codexTicketCompletedResponse(model, fakeCodexTicketState(length))
	}}
	svc.httpUpstream = upstream
	return svc, repo, upstream
}

func TestCodexTicketAutoAcceptsDifferentLengthsAcrossTiersAndModels(t *testing.T) {
	for _, tier := range []string{"pro", "team", "future-tier"} {
		for _, length := range []int{292, 312, 332} {
			for _, model := range []string{"gpt-6-astra", "gpt-5.6-sol"} {
				t.Run(fmt.Sprintf("%s_%d_%s", tier, length, model), func(t *testing.T) {
					svc, repo, upstream := codexTicketAutoFixture(t, model, length)
					repo.account.Credentials["plan_type"] = tier
					svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, model)
					ticket := svc.lookupOpenAICodexTicket(repo.account, model)
					require.NotNil(t, ticket)
					require.True(t, ticket.Verified)
					require.NotEmpty(t, ticket.AccountBinding)
					require.Equal(t, length, ticket.Length)
					require.Len(t, upstream.requests, 2)
					require.Equal(t, ticket.State, upstream.requests[1].Header.Get(openAICodexTurnStateHeader))
					svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, model)
					require.Len(t, upstream.requests, 2, "随机打票优先复用经过复验的现有票")
					requireAutoTicketRuntimeAvailability(t, svc, repo.account, ticket, true)
				})
			}
		}
	}
}

func requireAutoTicketRuntimeAvailability(t *testing.T, svc *OpenAIGatewayService, account *Account, ticket *openAICodexTicket, available bool) {
	t.Helper()
	account.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
	cfg := svc.openAICodexTicketConfig()
	headers := http.Header{}
	require.Equal(t, available, svc.applyOpenAICodexTicket(context.Background(), account, ticket.Model, headers) == nil)
	require.Equal(t, !available, svc.openAICodexTicketBlocksAccount(account, ticket.Model))
	require.Equal(t, available, OpenAICodexTicketStatuses(account, cfg, time.Now())[0].Ready)
	ws := openAIWSAcquireRequest{}
	require.Equal(t, available, svc.refreshOpenAICodexTicketWSHeaders(context.Background(), account, ticket.Model, &ws) == nil)
	snapshot := NewSharedPoolTicketAccountSnapshot(account, time.Now())
	require.NotNil(t, snapshot)
	snapshot.Available, snapshot.Concurrency = true, 3
	capacity := &SharedPoolCapacity{AvailableAccounts: 1, ConcurrencyCapacity: 3, TicketAccounts: []SharedPoolTicketAccountSnapshot{*snapshot}}
	require.Equal(t, available, GetSharedPoolCatalogCapacity(capacity, cfg, time.Now()).AvailableAccounts == 1)
}

func TestCodexTicketAutoRejectsUnverifiedUnboundExpiredAndMalformedStoredTickets(t *testing.T) {
	for _, kind := range []string{"unverified", "unbound", "expired", "revoked", "wrong_length", "internal_space", "binding_changed", "wrong_model"} {
		t.Run(kind, func(t *testing.T) {
			svc, repo, _ := codexTicketAutoFixture(t, "gpt-6-astra", 312)
			ticket := &openAICodexTicket{Model: "gpt-6-astra", State: fakeCodexTicketState(312), Length: 312,
				Verified: true, CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), AccountBinding: openAICodexTicketAccountBinding(repo.account)}
			switch kind {
			case "unverified":
				ticket.Verified = false
			case "unbound":
				ticket.AccountBinding = ""
			case "expired":
				ticket.ExpiresAt = time.Now().Add(-time.Second)
			case "revoked":
				ticket.Revoked = true
			case "wrong_length":
				ticket.Length = 292
			case "internal_space":
				ticket.State = ticket.State[:100] + " " + ticket.State[101:]
			case "binding_changed":
				repo.account.Credentials["chatgpt_account_id"] = "changed"
			case "wrong_model":
				ticket.Model = "gpt-other"
			}
			if kind == "wrong_model" {
				repo.account.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
				require.ErrorIs(t, svc.applyOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra", http.Header{}), ErrOpenAICodexTicketUnavailable)
				return
			}
			requireAutoTicketRuntimeAvailability(t, svc, repo.account, ticket, false)
		})
	}
}

func TestCodexTicketAutoCandidateShapeIsBoundedAndCannotCarryHeaderControlCharacters(t *testing.T) {
	for _, state := range []string{fakeCodexTicketState(15), fakeCodexTicketState(8193), "", strings.Repeat("x", 312),
		"gAAAAA" + strings.Repeat("a", 50) + "\r\nHeader: injected", "gAAAAAinternal space", "gAAAAAinternal\tspace",
		"gAAAAAinternal\x00value", "gAAAAAunicode中文", "gAAAAA" + strings.Repeat("x", 20) + "=after", "gAAAAA" + strings.Repeat("x", 20) + "==="} {
		svc, repo, upstream := codexTicketAutoFixture(t, "gpt-6-astra", 312)
		upstream.respond = func(int) *http.Response { return codexTicketCompletedResponse("gpt-6-astra", state) }
		svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
		require.Len(t, upstream.requests, 1)
		require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
	}
	for _, state := range []string{fakeCodexTicketState(16), fakeCodexTicketState(8192), "gAAAAAabc_-012345=="} {
		require.True(t, codexTicketAutoStateShape(state))
	}
}

func TestCodexTicketAutoRequiresBothCompletedMatchingResponses(t *testing.T) {
	for _, phase := range []int{1, 2} {
		for _, kind := range []string{"model_mismatch", "truncated", "failed"} {
			t.Run(fmt.Sprintf("%d_%s", phase, kind), func(t *testing.T) {
				svc, repo, upstream := codexTicketAutoFixture(t, "gpt-6-astra", 312)
				upstream.respond = func(call int) *http.Response {
					response := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312))
					if call != phase {
						return response
					}
					switch kind {
					case "model_mismatch":
						return codexTicketCompletedResponse("gpt-other", fakeCodexTicketState(312))
					case "truncated":
						response.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.created\"}\n\n"))
					case "failed":
						response.Body = io.NopCloser(strings.NewReader("data: {\"type\":\"response.failed\"}\n\n"))
					}
					return response
				}
				svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
				require.Len(t, upstream.requests, phase)
				require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
			})
		}
	}
}

func TestCodexTicketAutoPublishesVerifiedCandidateInsteadOfNewBusinessResponseState(t *testing.T) {
	svc, repo, upstream := codexTicketAutoFixture(t, "gpt-6-astra", 292)
	upstream.respond = func(call int) *http.Response {
		length := 292
		if call == 2 {
			length = 312
		}
		return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(length))
	}
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Len(t, upstream.requests, 2)
	require.Equal(t, fakeCodexTicketState(292), svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra").State)
}

func TestCodexTicketAutoSwitchStopsInFlightAndStrictStillRejects312(t *testing.T) {
	for _, phase := range []int{0, 1, 2} {
		svc, repo, upstream := codexTicketAutoFixture(t, "gpt-6-astra", 312)
		if phase == 0 {
			svc.cfg.Gateway.OpenAICodexTicket.LengthMode = config.CodexTicketLengthStrict
		}
		upstream.respond = func(call int) *http.Response {
			if call == phase {
				svc.cfg.Gateway.OpenAICodexTicket.LengthMode = config.CodexTicketLengthStrict
			}
			return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312))
		}
		svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
		require.Len(t, upstream.requests, max(1, phase))
		require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
	}
}
