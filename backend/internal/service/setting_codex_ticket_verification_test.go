package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketBusinessVerificationLegacyPolicyDefaults(t *testing.T) {
	svc, repo := newTicketPolicySettings()
	cfg, err := svc.GetCodexTicketSettings(context.Background())
	require.NoError(t, err)
	cfg.VerifyBusiness = nil
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	repo.values[SettingKeyCodexTicketPolicy] = string(raw)
	loaded, err := svc.GetCodexTicketSettings(context.Background())
	require.NoError(t, err)
	require.NotNil(t, loaded.VerifyBusiness)
	require.True(t, config.CodexTicketBusinessVerificationEnabled(loaded))
}

func TestCodexTicketBusinessVerificationRoundTripAndSnapshotIsolation(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTicketPolicySettings()
	cfg, err := svc.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	disabled := false
	cfg.VerifyBusiness = &disabled
	saved, err := svc.UpdateCodexTicketSettings(ctx, cfg)
	require.NoError(t, err)
	require.Contains(t, repo.values[SettingKeyCodexTicketPolicy], `"verify_business":false`)
	disabled = true
	*saved.VerifyBusiness = true
	runtime := svc.GetOpenAICodexTicketRuntimeConfig(ctx, cfg)
	require.False(t, config.CodexTicketBusinessVerificationEnabled(runtime))
	*runtime.VerifyBusiness = true
	require.False(t, config.CodexTicketBusinessVerificationEnabled(svc.GetOpenAICodexTicketRuntimeConfig(ctx, cfg)))
	restarted := NewSettingService(repo, svc.cfg)
	loaded, err := restarted.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	require.False(t, config.CodexTicketBusinessVerificationEnabled(loaded))
	*loaded.VerifyBusiness = true
	_, err = restarted.UpdateCodexTicketSettings(ctx, loaded)
	require.NoError(t, err)
	require.True(t, config.CodexTicketBusinessVerificationEnabled(restarted.GetOpenAICodexTicketRuntimeConfig(ctx, cfg)))
}
