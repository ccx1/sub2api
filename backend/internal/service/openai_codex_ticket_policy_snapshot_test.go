package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketWSInitialHandshakeRetainsInjectionSnapshotAcrossPolicyChanges(t *testing.T) {
	for _, change := range []string{"disabled", "model_removed", "length_changed"} {
		t.Run(change, func(t *testing.T) {
			svc, account, previous := codexTicketWSFixture(t)
			var receipt *openAICodexTicketWSReceipt
			headers, _, err := svc.buildOpenAIWSHeaders(context.Background(), nil, account, "tok", OpenAIWSProtocolDecision{}, true, "", "", "", previous.ticket.Model, "", &receipt)
			require.NoError(t, err)
			require.NotNil(t, receipt)
			require.Equal(t, previous.ticket.State, headers.Get(openAICodexTurnStateHeader))
			switch change {
			case "disabled":
				svc.cfg.Gateway.OpenAICodexTicket.Enabled = false
			case "model_removed":
				svc.cfg.Gateway.OpenAICodexTicket.Models = []string{"other-model"}
			case "length_changed":
				svc.cfg.Gateway.OpenAICodexTicket.TargetLength = 332
			}
			// 首握手仍观察实际注入的票，热更新不能让 receipt 消失。
			receipt.observeHandshake(context.Background(), svc, http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{fakeCodexTicketState(312)}})
			require.Eventually(t, func() bool { return svc.lookupOpenAICodexTicket(account, previous.ticket.Model) == nil }, time.Second, time.Millisecond)
		})
	}
}

func TestCodexTicketRejectedLengthUsesRequestSnapshot(t *testing.T) {
	svc, account, ticket, _ := ticketWatchdogFixture(t)
	svc.cfg.Gateway.OpenAICodexTicket.RejectedLengths = []int{352}
	req, _ := http.NewRequest(http.MethodPost, "http://localhost", nil)
	require.NoError(t, svc.applyOpenAICodexTicketRequest(account, ticket.Model, req))
	// 后续配置把拒绝长度改为空，也不改写已出站请求的判定快照。
	svc.cfg.Gateway.OpenAICodexTicket.RejectedLengths = []int{}
	response := codexTicketCompletedResponse(ticket.Model, fakeCodexTicketState(352))
	svc.observeOpenAICodexTicketResponse(req, response)
	defer response.Body.Close()
	require.Eventually(t, func() bool { return svc.lookupOpenAICodexTicket(account, ticket.Model) == nil }, time.Second, time.Millisecond)
}

func TestCodexTicketRetryPolicyUpdatePreservesDeadlineAndAffectsNextFailure(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{RetryBackoffSeconds: []int{30, 60}}, nil)
	input, now := codexTicketBackoffInput(), time.Now()
	svc.advanceCodexTicketBackoff(input, now)
	key := codexTicketBackoffKey(input.Account, input.Token, input.Model)
	raw, _ := svc.openaiCodexTicketBackoff.Load(key)
	previous := raw.(*codexTicketBackoffState)
	svc.cfg.Gateway.OpenAICodexTicket.RetryBackoffSeconds = []int{5, 10}
	require.True(t, svc.openAICodexTicketBackoffActive(input.Account, input.Token, input.Model))
	raw, _ = svc.openaiCodexTicketBackoff.Load(key)
	require.Equal(t, previous.RetryAt, raw.(*codexTicketBackoffState).RetryAt)
	nextFailure := previous.RetryAt.Add(time.Second)
	svc.advanceCodexTicketBackoff(input, nextFailure)
	raw, _ = svc.openaiCodexTicketBackoff.Load(key)
	require.Equal(t, nextFailure.Add(10*time.Second), raw.(*codexTicketBackoffState).RetryAt)
}

func TestCodexTicketFinalPublicationRejectsChangedPolicy(t *testing.T) {
	for _, change := range []string{"disabled", "model_removed", "length_changed", "tier_changed"} {
		t.Run(change, func(t *testing.T) {
			svc, account, previous := codexTicketWSFixture(t)
			ticket := previous.ticket
			ticket.Verified, ticket.Binding = true, svc.codexTicketBinding(account)
			switch change {
			case "disabled":
				svc.cfg.Gateway.OpenAICodexTicket.Enabled = false
			case "model_removed":
				svc.cfg.Gateway.OpenAICodexTicket.Models = []string{"other-model"}
			case "length_changed":
				svc.cfg.Gateway.OpenAICodexTicket.TargetLength = 332
			case "tier_changed":
				account.Credentials["plan_type"] = "team"
			}
			require.False(t, svc.storeOpenAICodexTicket(context.Background(), account, &ticket))
		})
	}
}
