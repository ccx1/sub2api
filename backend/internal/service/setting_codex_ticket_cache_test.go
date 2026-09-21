package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketPolicyReadFailureNeverDisablesSavedGate(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTicketPolicySettings()
	cfg, _ := svc.GetCodexTicketSettings(ctx)
	cfg.Enabled = true
	_, err := svc.UpdateCodexTicketSettings(ctx, cfg)
	require.NoError(t, err)
	repo.err = errors.New("database temporarily unavailable")
	require.True(t, svc.GetOpenAICodexTicketRuntimeConfig(ctx, config.OpenAICodexTicketConfig{}).Enabled)
	svc.InvalidateOpenAICodexTicketEnabledCache()
	require.True(t, svc.GetOpenAICodexTicketRuntimeConfig(ctx, config.OpenAICodexTicketConfig{}).Enabled)
}

func TestCodexTicketPolicyInvalidationWithoutKnownValueUsesFallback(t *testing.T) {
	svc, repo := newTicketPolicySettings()
	svc.InvalidateOpenAICodexTicketEnabledCache()
	repo.err = errors.New("database temporarily unavailable")
	require.True(t, svc.GetOpenAICodexTicketEnabled(context.Background(), true))
	// 关闭成功后也不能因为旧 YAML 开着而重新开始探测。
	repo.err = nil
	cfg, _ := svc.GetCodexTicketSettings(context.Background())
	cfg.Enabled = false
	_, err := svc.UpdateCodexTicketSettings(context.Background(), cfg)
	require.NoError(t, err)
	svc.InvalidateOpenAICodexTicketEnabledCache()
	repo.err = errors.New("database temporarily unavailable")
	require.False(t, svc.GetOpenAICodexTicketEnabled(context.Background(), true))
}

func TestCodexTicketPolicySystemSettingsSavePublishesEnabledValue(t *testing.T) {
	ctx := context.Background()
	svc, repo := newTicketPolicySettings()

	// 模拟保存前缓存中的旧值。系统设置保存成功后，后续读库故障也必须保留新值。
	svc.cacheOpenAICodexTicketEnabled(false)
	require.NoError(t, svc.UpdateSettings(ctx, &SystemSettings{OpenAICodexTicketEnabled: true}))
	svc.InvalidateOpenAICodexTicketEnabledCache()
	repo.err = errors.New("database temporarily unavailable")

	require.True(t, svc.GetOpenAICodexTicketEnabled(ctx, false))
}
