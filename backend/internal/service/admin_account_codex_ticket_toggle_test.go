package service

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpdateAccountExtraCodexTicketTogglePreservesSavedTicket(t *testing.T) {
	for _, initial := range []struct {
		name     string
		explicit bool
	}{{"legacy default enabled", false}, {"explicit enabled", true}} {
		t.Run(initial.name, func(t *testing.T) {
			account, wantTicket := codexTicketToggleTestAccount()
			if initial.explicit {
				account.Extra[OpenAICodexTicketEnabledExtraKey] = true
			}
			wantExtra := maps.Clone(account.Extra)
			wantCredentials := maps.Clone(account.Credentials)
			wantProxy := *account.Proxy
			repo := &upstreamBillingProbeAccountRepo{accounts: map[int64]*Account{account.ID: account}}
			svc := &adminServiceImpl{accountRepo: repo}
			for _, enabled := range []bool{false, true} {
				patch := map[string]any{OpenAICodexTicketEnabledExtraKey: enabled}
				require.NoError(t, svc.UpdateAccountExtra(context.Background(), account.ID, patch))
				updated, err := repo.GetByID(context.Background(), account.ID)
				require.NoError(t, err)
				wantExtra[OpenAICodexTicketEnabledExtraKey] = enabled
				require.Equal(t, wantExtra, updated.Extra)
				stored := parseOpenAICodexTicketFromAny(account.ID, wantTicket.Model, updated.Extra[openAICodexTicketExtraKey(wantTicket.Model)])
				require.Equal(t, wantTicket, stored)
				require.Equal(t, wantCredentials, updated.Credentials)
				require.EqualValues(t, wantProxy.ID, *updated.ProxyID)
				require.Equal(t, wantProxy, *updated.Proxy)
				require.Equal(t, patch, repo.updates[account.ID][len(repo.updates[account.ID])-1])
			}
			require.Len(t, repo.updates[account.ID], 2)
		})
	}
}

func codexTicketToggleTestAccount() (*Account, *openAICodexTicket) {
	account := ticketTestAccount(41)
	proxyID := int64(9)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "account-proxy.example", Port: 8080}
	capturedAt := time.Now().UTC().Add(-time.Minute)
	ticket := &openAICodexTicket{AccountID: account.ID, Model: "gpt-6-astra", State: fakeCodexTicketState(292),
		Length: 292, CapturedAt: capturedAt, ExpiresAt: capturedAt.Add(time.Hour)}
	stored := *ticket
	account.Extra = map[string]any{
		openAICodexTicketExtraKey(ticket.Model): &stored,
		CodexTicketProxyModeExtraKey:            CodexTicketProxyModeFixed,
		CodexTicketProxyIDExtraKey:              int64(7),
		"enable_tls_fingerprint":                true,
		"tls_fingerprint_builtin":               "nodejs24",
		"tls_fingerprint_profile_id":            int64(3),
		"codex_fingerprint_mode":                "device",
		codexFingerprintSeedExtraKey:            "synthetic-fingerprint-seed",
	}
	return account, ticket
}
