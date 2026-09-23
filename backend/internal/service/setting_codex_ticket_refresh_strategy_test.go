package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketRefreshStrategySaveReloadAndLegacyDefault(t *testing.T) {
	for _, mode := range []string{"", config.CodexTicketRefreshRevalidate, config.CodexTicketRefreshReplace} {
		svc, repo := newTicketPolicySettings()
		ctx := context.Background()
		cfg, err := svc.GetCodexTicketSettings(ctx)
		require.NoError(t, err)
		cfg.RefreshStrategy = mode
		raw, err := json.Marshal(cfg)
		require.NoError(t, err)
		repo.values[SettingKeyCodexTicketPolicy] = string(raw)
		want := mode
		if want == "" {
			want = config.CodexTicketRefreshRevalidate
		}
		loaded, err := svc.GetCodexTicketSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, want, loaded.RefreshStrategy)
		saved, err := svc.UpdateCodexTicketSettings(ctx, loaded)
		require.NoError(t, err)
		require.Equal(t, want, saved.RefreshStrategy)
		restarted := NewSettingService(repo, svc.cfg)
		reloaded, err := restarted.GetCodexTicketSettings(ctx)
		require.NoError(t, err)
		require.Equal(t, want, reloaded.RefreshStrategy)
		require.Equal(t, want, restarted.GetOpenAICodexTicketRuntimeConfig(ctx, cfg).RefreshStrategy)
	}
}

func TestCodexTicketRefreshStrategyRejectsUnknownWithoutWriting(t *testing.T) {
	svc, repo := newTicketPolicySettings()
	cfg, err := svc.GetCodexTicketSettings(context.Background())
	require.NoError(t, err)
	cfg.RefreshStrategy = "wait_for_ticket"
	_, err = svc.UpdateCodexTicketSettings(context.Background(), cfg)
	require.Error(t, err)
	require.Zero(t, repo.writes)
}

func TestCodexTicketRefreshStrategySwitchKeepsTicketAndFencesOldProbe(t *testing.T) {
	svc, account, old := codexRevalidationFixture(t, "")
	captured := svc.openAICodexTicketConfigForAccount(context.Background(), account)
	legacy := captured
	legacy.RefreshStrategy = ""
	legacyBinding := svc.codexTicketBindingForConfig(account, legacy)
	for _, mode := range []string{config.CodexTicketRefreshRevalidate, config.CodexTicketRefreshReplace} {
		svc.cfg.Gateway.OpenAICodexTicket.RefreshStrategy = mode
		cfg := svc.openAICodexTicketConfigForAccount(context.Background(), account)
		require.Equal(t, legacyBinding, svc.codexTicketBindingForConfig(account, cfg))
		ticket := svc.lookupOpenAICodexTicket(account, old.Model)
		require.NotNil(t, ticket)
		require.True(t, ticket.usable(time.Now(), account, cfg))
	}
	input := openAICodexTicketProbeInput{Account: account, Config: &captured, SubscriptionTier: openAICodexTicketSubscriptionTier(account)}
	require.False(t, svc.openAICodexTicketProbeConfigCurrent(context.Background(), input))
}
