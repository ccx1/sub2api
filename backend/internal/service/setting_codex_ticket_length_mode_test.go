package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketLengthModePersistenceAndLegacyRead(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTicketPolicySettings()
	legacy, err := svc.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	encoded, err := json.Marshal(legacy)
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(encoded, &document))
	delete(document, "length_mode")
	encoded, err = json.Marshal(document)
	require.NoError(t, err)
	repo.values[SettingKeyCodexTicketPolicy] = string(encoded)
	loaded, err := svc.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, config.CodexTicketLengthStrict, loaded.LengthMode)
	loaded.LengthMode = config.CodexTicketLengthAuto
	stored, err := svc.UpdateCodexTicketSettings(ctx, loaded)
	require.NoError(t, err)
	require.Equal(t, stored, svc.GetOpenAICodexTicketRuntimeConfig(ctx, legacy))
	restarted := NewSettingService(repo, svc.cfg)
	reloaded, err := restarted.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, config.CodexTicketLengthAuto, reloaded.LengthMode)
	reloaded.LengthMode = config.CodexTicketLengthStrict
	_, err = restarted.UpdateCodexTicketSettings(ctx, reloaded)
	require.NoError(t, err)
	require.Equal(t, legacy.TierRules, reloaded.TierRules)
	require.Equal(t, legacy.RejectedLengths, reloaded.RejectedLengths)
}

func TestCodexTicketLengthModeValidation(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTicketPolicySettings()
	cfg, err := svc.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	cfg.LengthMode = "unknown"
	_, err = svc.UpdateCodexTicketSettings(ctx, cfg)
	require.Error(t, err)
	require.Zero(t, repo.writes)
	cfg.LengthMode = config.CodexTicketLengthAuto
	cfg.TargetLength, cfg.TierRules[0].TargetLength = 312, 312
	_, err = svc.UpdateCodexTicketSettings(ctx, cfg)
	require.NoError(t, err)
	require.Equal(t, 1, repo.writes)
	cfg.LengthMode = config.CodexTicketLengthStrict
	_, err = svc.UpdateCodexTicketSettings(ctx, cfg)
	require.Error(t, err)
	require.Equal(t, 1, repo.writes)
	require.Equal(t, config.CodexTicketLengthAuto, svc.GetOpenAICodexTicketRuntimeConfig(ctx, cfg).LengthMode)
}
