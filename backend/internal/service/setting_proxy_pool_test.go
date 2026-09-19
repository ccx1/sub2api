package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestProxyPoolSettingsRuntimeDefaultsAndExplicitMode(t *testing.T) {
	for _, tc := range []struct {
		name, storedURL, yamlURL, mode, want string
	}{
		{name: "empty pool", want: "pool"},
		{name: "stored fixed", storedURL: "http://proxy.example.com:8080", want: "fixed"},
		{name: "yaml fixed", yamlURL: "http://proxy.example.com:8080", want: "fixed"},
		{name: "explicit overrides legacy", storedURL: "http://proxy.example.com:8080", mode: "pool", want: "pool"},
		{name: "explicit fixed", mode: "fixed", want: "fixed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
				SettingKeyOpenAICodexTicketHarvestProxyURL: tc.storedURL, SettingKeyOpenAICodexTicketHarvestProxyMode: tc.mode,
			}}}
			cfg := &config.Config{}
			cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = tc.yamlURL
			svc := NewSettingService(repo, cfg)
			mode, err := svc.GetOpenAICodexTicketHarvestProxyMode(context.Background())
			require.NoError(t, err)
			require.Equal(t, tc.want, mode)
			max, err := svc.GetProxyPoolMaxAccounts(context.Background())
			require.NoError(t, err)
			require.Zero(t, max)
		})
	}
}

func TestProxyPoolSettingsCacheExpiryAndInvalidation(t *testing.T) {
	repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
		SettingKeyOpenAICodexTicketHarvestProxyMode: "pool", SettingKeyProxyPoolMaxAccounts: "2",
	}}}
	svc := NewSettingService(repo, &config.Config{})
	max, err := svc.GetProxyPoolMaxAccounts(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, max)
	repo.values[SettingKeyProxyPoolMaxAccounts] = "3"
	max, err = svc.GetProxyPoolMaxAccounts(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, max)
	svc.proxyPoolSettingsCache.expiresAt = time.Now().Add(-time.Second)
	max, err = svc.GetProxyPoolMaxAccounts(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, max)
	repo.values[SettingKeyProxyPoolMaxAccounts] = "4"
	svc.InvalidateProxyPoolSettingsCache()
	max, err = svc.GetProxyPoolMaxAccounts(context.Background())
	require.NoError(t, err)
	require.Equal(t, 4, max)
}

func TestProxyPoolSettingsStorageFailureDoesNotChangeRoutingPolicy(t *testing.T) {
	repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
		SettingKeyOpenAICodexTicketHarvestProxyMode: "pool", SettingKeyProxyPoolMaxAccounts: "2",
	}}}
	cfg := &config.Config{}
	cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = "http://fixed.example.com:8080"
	svc := NewSettingService(repo, cfg)
	mode, err := svc.GetOpenAICodexTicketHarvestProxyMode(context.Background())
	require.NoError(t, err)
	require.Equal(t, "pool", mode)
	svc.proxyPoolSettingsCache.expiresAt = time.Now().Add(-time.Second)
	repo.err = errors.New("storage unavailable")
	mode, err = svc.GetOpenAICodexTicketHarvestProxyMode(context.Background())
	require.ErrorIs(t, err, repo.err)
	require.Empty(t, mode)
	_, err = svc.GetProxyPoolMaxAccounts(context.Background())
	require.ErrorIs(t, err, repo.err)
}

func TestProxyPoolSettingsRejectMalformedPersistedValues(t *testing.T) {
	repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
		SettingKeyOpenAICodexTicketHarvestProxyMode: "direct",
	}}}
	svc := NewSettingService(repo, &config.Config{})
	_, err := svc.GetOpenAICodexTicketHarvestProxyMode(context.Background())
	require.Error(t, err)
	for _, raw := range []string{"-1", "10001", "invalid", "1.5"} {
		repo.values[SettingKeyProxyPoolMaxAccounts] = raw
		svc.InvalidateProxyPoolSettingsCache()
		_, err = svc.GetProxyPoolMaxAccounts(context.Background())
		require.Error(t, err)
	}
}

func TestProxyPoolSettingsCanceledContextAndMissingRepository(t *testing.T) {
	repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{}}}
	svc := NewSettingService(repo, &config.Config{})
	_, err := svc.GetOpenAICodexTicketHarvestProxyMode(context.Background())
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = svc.GetOpenAICodexTicketHarvestProxyMode(ctx)
	require.ErrorIs(t, err, context.Canceled)
	_, err = svc.GetProxyPoolMaxAccounts(ctx)
	require.ErrorIs(t, err, context.Canceled)
	var missing *SettingService
	_, err = missing.GetProxyPoolMaxAccounts(context.Background())
	require.Error(t, err)
}
