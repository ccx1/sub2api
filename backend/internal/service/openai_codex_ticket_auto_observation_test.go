package service

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketAutoHTTPKeepsLengthChangeButRevokesModelMismatch(t *testing.T) {
	svc, repo, _ := codexTicketAutoFixture(t, "gpt-6-astra", 292)
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	req, err := http.NewRequest(http.MethodPost, "http://localhost", nil)
	require.NoError(t, err)
	require.NoError(t, svc.applyOpenAICodexTicketRequest(repo.account, "gpt-6-astra", req))
	response := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312))
	svc.observeOpenAICodexTicketResponse(req, response)
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	ticket := svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra")
	require.NotNil(t, ticket)
	require.Equal(t, fakeCodexTicketState(292), ticket.State)
	response = codexTicketCompletedResponse("gpt-other", fakeCodexTicketState(312))
	svc.observeOpenAICodexTicketResponse(req, response)
	_, err = io.ReadAll(response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Eventually(t, func() bool { return svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra") == nil }, time.Second, time.Millisecond)
}

func TestCodexTicketAutoWSKeepsLengthChangeButRevokesModelMismatch(t *testing.T) {
	svc, repo, _ := codexTicketAutoFixture(t, "gpt-6-astra", 312)
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	headers := http.Header{}
	snapshot, err := svc.applyOpenAICodexTicketSnapshot(context.Background(), repo.account, "gpt-6-astra", headers)
	require.NoError(t, err)
	receipt := codexTicketWSReceiptFromSnapshot(snapshot)
	require.NotNil(t, receipt)
	receipt.observeHandshake(context.Background(), svc, http.Header{http.CanonicalHeaderKey(openAICodexTurnStateHeader): []string{fakeCodexTicketState(312)}})
	require.NotNil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
	watch := receipt.watch(context.Background(), svc, "gpt-6-astra")
	watch.invalidate = func() { svc.invalidateOpenAICodexTicket(context.Background(), repo.account, &receipt.ticket) }
	watch.observe([]byte(`{"type":"response.completed","response":{"status":"completed","model":"gpt-6-astra"}}`))
	require.NotNil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
	watch.observe([]byte(`{"type":"response.completed","response":{"status":"completed","model":"gpt-other"}}`))
	require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
}

func TestCodexTicketLengthModeObservationsUseInjectedSnapshot(t *testing.T) {
	for _, automatic := range []bool{false, true} {
		for _, transport := range []string{"http", "ws"} {
			svc, repo, _ := codexTicketAutoFixture(t, "gpt-6-astra", 292)
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			if !automatic {
				svc.cfg.Gateway.OpenAICodexTicket.LengthMode = config.CodexTicketLengthStrict
			}
			req, err := http.NewRequest(http.MethodPost, "http://localhost", nil)
			require.NoError(t, err)
			receipt, err := svc.applyOpenAICodexTicketSnapshot(context.Background(), repo.account, "gpt-6-astra", req.Header)
			require.NoError(t, err)
			if automatic {
				svc.cfg.Gateway.OpenAICodexTicket.LengthMode = config.CodexTicketLengthStrict
			} else {
				svc.cfg.Gateway.OpenAICodexTicket.LengthMode = config.CodexTicketLengthAuto
			}
			response := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(312))
			if transport == "http" {
				req = req.WithContext(context.WithValue(req.Context(), openAICodexTicketReceiptKey{}, receipt))
				svc.observeOpenAICodexTicketResponse(req, response)
				_, err = io.ReadAll(response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
			} else {
				codexTicketWSReceiptFromSnapshot(receipt).observeHandshake(context.Background(), svc, response.Header)
				require.NoError(t, response.Body.Close())
			}
			if automatic {
				require.NotNil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
			} else {
				require.Eventually(t, func() bool { return svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra") == nil }, time.Second, time.Millisecond)
			}
		}
	}
}

func TestSharedPoolTicketAutoPolicyChangeUsesVerifiedSnapshotMetadata(t *testing.T) {
	svc, repo, _ := codexTicketAutoFixture(t, "gpt-6-astra", 312)
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	ticket := svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra")
	require.NotNil(t, ticket)
	repo.account.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
	snapshot := NewSharedPoolTicketAccountSnapshot(repo.account, time.Now())
	snapshot.Available, snapshot.Concurrency = true, 3
	capacity := &SharedPoolCapacity{AvailableAccounts: 1, ConcurrencyCapacity: 3, TicketAccounts: []SharedPoolTicketAccountSnapshot{*snapshot}}
	cfg := svc.openAICodexTicketConfig()
	require.EqualValues(t, 1, GetSharedPoolCatalogCapacity(capacity, cfg, time.Now()).AvailableAccounts)
	cfg.LengthMode = config.CodexTicketLengthStrict
	require.Zero(t, GetSharedPoolCatalogCapacity(capacity, cfg, time.Now()).AvailableAccounts)
	cfg.LengthMode, cfg.TargetLength, cfg.RejectedLengths = config.CodexTicketLengthAuto, 512, []int{292, 312, 332}
	cfg.TierRules = []config.CodexTicketTierRule{{Tier: "team", TargetLength: 1024}}
	require.EqualValues(t, 1, GetSharedPoolCatalogCapacity(capacity, cfg, time.Now()).AvailableAccounts)
	require.Zero(t, GetSharedPoolCatalogCapacity(capacity, cfg, ticket.ExpiresAt).AvailableAccounts)
}
