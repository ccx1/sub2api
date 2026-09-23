package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type codexTicketRetryRepo struct {
	AccountRepository
	account *Account
}

func (r *codexTicketRetryRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}

func TestCodexTicketRetryModelsRequiresConfiguredModel(t *testing.T) {
	cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"gpt-6-astra", "gpt-5.6-sol"}})
	models, err := codexTicketRetryModels(cfg, "gpt-6-astra")
	require.NoError(t, err)
	require.Equal(t, []string{"gpt-6-astra"}, models)
	_, err = codexTicketRetryModels(cfg, "gpt-unknown")
	require.ErrorIs(t, err, ErrCodexTicketRetryModel)
}

func TestCodexTicketRetrySchedulesEveryConfiguredModel(t *testing.T) {
	account := ticketTestAccount(41)
	account.Status = StatusActive
	repo := &codexTicketRetryRepo{account: account}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, Models: []string{"gpt-6-astra", "gpt-5.6-sol"},
		HarvestProxyURL: "http://harvest.example:8080", RefreshBeforeSeconds: 300,
	}, nil)
	svc.accountRepo = repo

	state := fakeCodexTicketState(292)
	account.Extra = map[string]any{openAICodexTicketExtraKey("gpt-5.6-sol"): &openAICodexTicket{
		AccountID: account.ID, Model: "gpt-5.6-sol", State: state, Length: len(state), Verified: true,
		CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), AccountBinding: openAICodexTicketAccountBinding(account),
	}}
	svc.openaiCodexTicketBackoff.Store(codexTicketBackoffKey(account, "tok", "gpt-6-astra"), &codexTicketBackoffState{Failures: 3, RetryAt: time.Now().Add(time.Hour)})
	result, err := svc.RetryOpenAICodexTicket(context.Background(), account.ID, "")
	require.NoError(t, err)
	require.Equal(t, 2, result.Scheduled)
	require.Zero(t, result.Skipped)
	require.Equal(t, []string{"gpt-6-astra", "gpt-5.6-sol"}, result.Models)
	_, exists := svc.openaiCodexTicketBackoff.Load(codexTicketBackoffKey(account, "tok", "gpt-6-astra"))
	require.False(t, exists)
}

func TestCodexTicketManualRetryContextOnlyBypassesModelBackoff(t *testing.T) {
	ctx := withCodexTicketManualRetry(context.Background())
	require.True(t, isCodexTicketManualRetry(ctx))
	require.False(t, isCodexTicketManualRetry(context.Background()))
}

func TestCodexTicketRetrySchedulesExpiringTicketEvenForRandomProxy(t *testing.T) {
	account := ticketTestAccount(42)
	account.Status = StatusActive
	account.Extra = map[string]any{CodexTicketProxyModeExtraKey: CodexTicketProxyModeRandom}
	repo := &codexTicketRetryRepo{account: account}
	cfg := config.OpenAICodexTicketConfig{
		Enabled: true, Models: []string{"gpt-6-astra"}, RefreshBeforeSeconds: 300,
		LengthMode: config.CodexTicketLengthAuto,
	}
	svc := ticketTestService(t, cfg, nil)
	svc.accountRepo = repo
	state := fakeCodexTicketState(312)
	account.Extra[openAICodexTicketExtraKey("gpt-6-astra")] = &openAICodexTicket{
		AccountID: account.ID, Model: "gpt-6-astra", State: state, Length: len(state), Verified: true,
		CapturedAt: time.Now().Add(-time.Hour), ExpiresAt: time.Now().Add(time.Minute), AccountBinding: openAICodexTicketAccountBinding(account),
	}

	result, err := svc.RetryOpenAICodexTicket(context.Background(), account.ID, "")
	require.NoError(t, err)
	require.Equal(t, 1, result.Scheduled)
	require.Equal(t, []string{"gpt-6-astra"}, result.Models)
}

func TestCodexTicketRetryDoesNotRequireCachedAccessToken(t *testing.T) {
	account := ticketTestAccount(43)
	account.Status = StatusActive
	delete(account.Credentials, "access_token")
	account.Credentials["refresh_token"] = "refresh-token"
	repo := &codexTicketRetryRepo{account: account}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{
		Enabled: true, Models: []string{"gpt-6-astra"}, RefreshBeforeSeconds: 300,
	}, nil)
	svc.accountRepo = repo

	result, err := svc.RetryOpenAICodexTicket(context.Background(), account.ID, "")
	require.NoError(t, err)
	require.Equal(t, 1, result.Scheduled)
	require.Equal(t, []string{"gpt-6-astra"}, result.Models)
}
