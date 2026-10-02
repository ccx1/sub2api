package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketUsageSettingsPersistenceAndLegacyRead(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTicketPolicySettings()
	legacy, err := svc.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, config.CodexTicketUsageImmediate, legacy.UsageMode, "默认即取即用")
	require.Zero(t, legacy.MinTicketAgeSeconds)
	require.False(t, legacy.ConsumeAfterUse)

	// 升级前保存的规则没有取票字段，读取后按即取即用处理。
	encoded, err := json.Marshal(legacy)
	require.NoError(t, err)
	var document map[string]any
	require.NoError(t, json.Unmarshal(encoded, &document))
	require.Equal(t, config.CodexTicketUsageImmediate, document["usage_mode"])
	for _, key := range []string{"usage_mode", "min_ticket_age_seconds", "consume_after_use"} {
		delete(document, key)
	}
	encoded, err = json.Marshal(document)
	require.NoError(t, err)
	repo.values[SettingKeyCodexTicketPolicy] = string(encoded)
	loaded, err := svc.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, config.CodexTicketUsageImmediate, loaded.UsageMode)
	require.Zero(t, loaded.MinTicketAgeSeconds)
	require.False(t, loaded.ConsumeAfterUse)

	loaded.UsageMode, loaded.MinTicketAgeSeconds, loaded.ConsumeAfterUse = config.CodexTicketUsageAged, 300, true
	stored, err := svc.UpdateCodexTicketSettings(ctx, loaded)
	require.NoError(t, err)
	require.Equal(t, config.CodexTicketUsageAged, stored.UsageMode)
	runtime := svc.GetOpenAICodexTicketRuntimeConfig(ctx, legacy)
	require.Equal(t, config.CodexTicketUsageAged, runtime.UsageMode)
	require.Equal(t, 300, runtime.MinTicketAgeSeconds)
	require.True(t, runtime.ConsumeAfterUse)

	restarted := NewSettingService(repo, svc.cfg)
	reloaded, err := restarted.GetCodexTicketSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, config.CodexTicketUsageAged, reloaded.UsageMode)
	require.Equal(t, 300, reloaded.MinTicketAgeSeconds)
	require.True(t, reloaded.ConsumeAfterUse)
}

func TestCodexTicketUsageSettingsValidation(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTicketPolicySettings()
	cfg, err := svc.GetCodexTicketSettings(ctx)
	require.NoError(t, err)

	invalid := []struct {
		mode string
		age  int
	}{
		{mode: "oldest"},
		{mode: config.CodexTicketUsageAged},
		{mode: config.CodexTicketUsageAged, age: -1},
		{mode: config.CodexTicketUsageAged, age: cfg.TTLSeconds},
		{mode: config.CodexTicketUsageAged, age: config.MaxCodexTicketMinAgeSeconds + 1},
	}
	for _, tc := range invalid {
		next := cfg
		next.UsageMode, next.MinTicketAgeSeconds = tc.mode, tc.age
		_, err = svc.UpdateCodexTicketSettings(ctx, next)
		require.Error(t, err, tc)
		require.Zero(t, repo.writes, "非法取票机制不写入")
	}

	// 即取即用忽略遗留的沉淀时长；空值按即取即用保存。
	next := cfg
	next.UsageMode, next.MinTicketAgeSeconds = config.CodexTicketUsageImmediate, 999
	stored, err := svc.UpdateCodexTicketSettings(ctx, next)
	require.NoError(t, err)
	require.Equal(t, config.CodexTicketUsageImmediate, stored.UsageMode)
	require.Zero(t, stored.MinTicketAgeSeconds)
	next.UsageMode, next.MinTicketAgeSeconds = "", 0
	stored, err = svc.UpdateCodexTicketSettings(ctx, next)
	require.NoError(t, err)
	require.Equal(t, config.CodexTicketUsageImmediate, stored.UsageMode)

	next.UsageMode, next.MinTicketAgeSeconds = config.CodexTicketUsageAged, cfg.TTLSeconds-1
	stored, err = svc.UpdateCodexTicketSettings(ctx, next)
	require.NoError(t, err)
	require.Equal(t, cfg.TTLSeconds-1, stored.MinTicketAgeSeconds)
}
