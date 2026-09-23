package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketProtectionEmptyLengthsSaveAndReload(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTicketPolicySettings()
	cfg, err := svc.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	protection := config.DefaultCodexTicketProtection()
	protection.RejectAndSilenceLengths = []int{}
	cfg.Protection = &protection
	saved, err := svc.UpdateCodexTicketSettings(ctx, cfg)
	require.NoError(t, err)
	require.Equal(t, []int{}, saved.Protection.RejectAndSilenceLengths)
	require.Contains(t, repo.values[SettingKeyCodexTicketPolicy], `"reject_and_silence_lengths":[]`)
	other := NewSettingService(repo, svc.cfg)
	loaded, err := other.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, []int{}, loaded.Protection.RejectAndSilenceLengths)
	runtime := other.GetOpenAICodexTicketRuntimeConfig(ctx, cfg)
	require.Equal(t, []int{}, runtime.Protection.RejectAndSilenceLengths)
}

func TestCodexTicketProtectionLegacyNullLengthsLoadAsEmpty(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTicketPolicySettings()
	cfg, err := svc.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	protection := config.DefaultCodexTicketProtection()
	protection.RejectAndSilenceLengths = nil
	cfg.Protection = &protection
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"reject_and_silence_lengths":null`)
	repo.values[SettingKeyCodexTicketPolicy] = string(raw)
	loaded, err := svc.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, []int{}, loaded.Protection.RejectAndSilenceLengths)
	saved, err := svc.UpdateCodexTicketSettings(ctx, loaded)
	require.NoError(t, err)
	require.Equal(t, []int{}, saved.Protection.RejectAndSilenceLengths)
	require.Contains(t, repo.values[SettingKeyCodexTicketPolicy], `"reject_and_silence_lengths":[]`)
}
