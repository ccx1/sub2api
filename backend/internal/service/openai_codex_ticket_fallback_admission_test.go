package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func codexFallbackAdmissionFixture(t *testing.T) (*OpenAIGatewayService, *transportTicketRepo, *Account) {
	t.Helper()
	svc, repo, upstream := transportTicketFixture(t)
	svc.cfg.Gateway.OpenAICodexTicket.FailClosed = true
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Equal(t, []string{repo.fallback.URL(), repo.fallback.URL()}, upstream.proxies)
	require.Len(t, repo.saved, 1)
	return svc, repo, repo.saved[0]
}

func TestCodexTicketFallbackAdmissionChecksActualExit(t *testing.T) {
	svc, repo, runtime := codexFallbackAdmissionFixture(t)
	ctx := context.Background()
	scheduler := &defaultOpenAIAccountScheduler{service: svc}
	ok, reason := scheduler.isAccountRequestCompatibleReason(ctx, repo.account,
		OpenAIAccountScheduleRequest{RequestedModel: "gpt-6-astra"})
	require.True(t, ok, "verified fallback rejected before egress resolution: %s", reason)
	require.False(t, svc.openAICodexTicketBlocksAccountContext(ctx, runtime, "gpt-6-astra"))
	selected := cloneOpenAICodexTicketAccount(repo.account)
	require.NoError(t, ResolveRandomProxyFromSource(ctx, selected, repo))
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	require.NoError(t, svc.applyOpenAICodexTicketRequest(selected, "gpt-6-astra", request))
	require.NotEmpty(t, request.Header.Get(openAICodexTurnStateHeader))
	require.EqualValues(t, 58, *selected.ProxyID)
	require.EqualValues(t, 70, *repo.account.ProxyID, "admission must not mutate the configured account")
	require.EqualValues(t, 70, *selected.ConfiguredProxySnapshot().ProxyID)
	require.Len(t, repo.saved, 1, "admission and injection must not persist the runtime proxy")
}

func TestCodexTicketFallbackAdmissionRequiresUsableTicket(t *testing.T) {
	for _, state := range []string{"missing", "expired", "revoked", "other_model"} {
		t.Run(state, func(t *testing.T) {
			svc, repo, runtime := codexFallbackAdmissionFixture(t)
			model := "gpt-6-astra"
			key := openAICodexTicketKey(runtime.ID, model)
			ticket := svc.lookupOpenAICodexTicket(runtime, model)
			require.NotNil(t, ticket)
			switch state {
			case "missing":
				svc.openaiCodexTickets.Delete(key)
			case "expired":
				ticket.ExpiresAt, ticket.StateExpiresAt = time.Now().Add(-time.Second), time.Now().Add(-time.Second)
				svc.openaiCodexTickets.Store(key, ticket)
			case "revoked":
				ticket.Revoked = true
				svc.openaiCodexTickets.Store(key, ticket)
			case "other_model":
				model = "gpt-5.6-sol"
			}
			require.True(t, svc.openAICodexTicketBlocksAccountContext(context.Background(), repo.account, model))
			_, err := svc.applyOpenAICodexTicketSnapshot(context.Background(), runtime, model, http.Header{})
			require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
		})
	}
}

func TestCodexTicketFallbackAdmissionRechecksChangedExit(t *testing.T) {
	for _, state := range []string{"restored", "different_proxy", "different_address"} {
		t.Run(state, func(t *testing.T) {
			svc, repo, _ := codexFallbackAdmissionFixture(t)
			ctx := context.Background()
			require.False(t, svc.openAICodexTicketBlocksAccountContext(ctx, repo.account, "gpt-6-astra"))
			changeCodexFallbackAdmissionExit(repo, state)
			require.True(t, svc.openAICodexTicketBlocksAccountContext(ctx, repo.account, "gpt-6-astra"))
			selected := cloneOpenAICodexTicketAccount(repo.account)
			require.NoError(t, ResolveRandomProxyFromSource(ctx, selected, repo))
			headers := http.Header{}
			_, err := svc.applyOpenAICodexTicketSnapshot(ctx, selected, "gpt-6-astra", headers)
			require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable, "final injection must not reuse the ticket for the previous exit")
			require.Empty(t, headers.Get(openAICodexTurnStateHeader))
			require.EqualValues(t, 70, *repo.account.ProxyID)
			require.Len(t, repo.saved, 1)
		})
	}
}

func TestCodexTicketFallbackAdmissionWSKeepsOnlyMatchingExit(t *testing.T) {
	for _, state := range []string{"restored", "different_proxy", "different_address", "configured_proxy"} {
		t.Run(state, func(t *testing.T) {
			svc, repo, runtime := codexFallbackAdmissionFixture(t)
			ctx := context.Background()
			request := openAIWSAcquireRequest{}
			require.NoError(t, svc.refreshOpenAICodexTicketWSHeaders(ctx, runtime, "gpt-6-astra", &request))
			require.NotNil(t, request.CodexTicketReceipt)
			require.NotEmpty(t, request.Headers.Get(openAICodexTurnStateHeader))
			require.EqualValues(t, 58, *request.CodexTicketReceipt.account.ProxyID)
			if state == "configured_proxy" {
				repo.account.ProxyID, repo.account.Proxy = &repo.fallback.ID, repo.fallback
			} else {
				changeCodexFallbackAdmissionExit(repo, state)
			}
			require.ErrorIs(t, svc.refreshOpenAICodexTicketWSHeaders(ctx, runtime, "gpt-6-astra", &request), ErrOpenAICodexTicketUnavailable)
			require.Empty(t, request.Headers.Get(openAICodexTurnStateHeader), "failed refresh cannot retain the old injected ticket")
			require.EqualValues(t, 70, *runtime.ConfiguredProxySnapshot().ProxyID)
			require.Len(t, repo.saved, 1)
		})
	}
}

func TestCodexTicketFallbackAdmissionRejectsUnavailableExit(t *testing.T) {
	svc, repo, runtime := codexFallbackAdmissionFixture(t)
	repo.fallback = nil
	require.True(t, svc.openAICodexTicketBlocksAccountContext(context.Background(), repo.account, "gpt-6-astra"))
	request := openAIWSAcquireRequest{}
	require.ErrorIs(t, svc.refreshOpenAICodexTicketWSHeaders(context.Background(), runtime, "gpt-6-astra", &request), ErrOpenAICodexTicketUnavailable)
	require.Empty(t, request.Headers.Get(openAICodexTurnStateHeader))
	require.EqualValues(t, 70, *repo.account.ProxyID)
	require.Len(t, repo.saved, 1)
}

func changeCodexFallbackAdmissionExit(repo *transportTicketRepo, state string) {
	if state == "restored" {
		repo.fallback = repo.proxies[70]
		return
	}
	next := *repo.fallback
	next.Host = "changed.invalid"
	if state == "different_proxy" {
		next.ID = 66
	}
	repo.fallback = &next
}
