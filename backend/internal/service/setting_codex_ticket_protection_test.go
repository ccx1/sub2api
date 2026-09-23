package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketProtectionValidationAndSnapshot(t *testing.T) {
	svc, repo := newTicketPolicySettings()
	cfg, err := svc.GetCodexTicketSettings(context.Background())
	require.NoError(t, err)
	require.False(t, cfg.TicketProtection().Enabled)
	p := config.DefaultCodexTicketProtection()
	cfg.Protection = &p
	for _, mutate := range []func(*config.CodexTicketProtectionConfig){
		func(p *config.CodexTicketProtectionConfig) { p.MaxAccountAttempts = 0 },
		func(p *config.CodexTicketProtectionConfig) { p.MaxPoolRounds = 101 },
		func(p *config.CodexTicketProtectionConfig) { p.ProxySilenceSeconds = 86401 },
		func(p *config.CodexTicketProtectionConfig) { p.AccountCooldownSeconds = 0 },
		func(p *config.CodexTicketProtectionConfig) { p.RejectAndSilenceLengths = []int{312, 312} },
	} {
		bad := cloneCodexTicketSettings(cfg)
		mutate(bad.Protection)
		_, err = svc.UpdateCodexTicketSettings(context.Background(), bad)
		require.Error(t, err)
		require.Zero(t, repo.writes)
	}
	cfg.Protection.Enabled = true
	cfg.Protection.RejectAndSilenceLengths = []int{cfg.TargetLength}
	require.Error(t, validateCodexTicketProtection(&cfg))
	cfg.LengthMode = config.CodexTicketLengthAuto
	require.NoError(t, validateCodexTicketProtection(&cfg))
	clone := cloneCodexTicketSettings(cfg)
	clone.Protection.RejectAndSilenceLengths[0] = 800
	require.NotEqual(t, clone.Protection.RejectAndSilenceLengths, cfg.Protection.RejectAndSilenceLengths)
}

func TestCodexTicketProtectionAndStrategyPreserveLegacyBinding(t *testing.T) {
	svc, repo := codexTicketControlService(t)
	cfg := svc.openAICodexTicketConfig()
	before := svc.codexTicketBindingForConfig(repo.account, cfg)
	encoded, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "protection")
	p := config.DefaultCodexTicketProtection()
	p.Enabled = true
	cfg.Protection = &p
	repo.account.Extra[CodexTicketProxyStrategyExtraKey] = CodexTicketProxyStrategyRoundRobin
	require.Equal(t, before, svc.codexTicketBindingForConfig(repo.account, cfg))
}

func TestCodexTicketProtectionDisableWatermarkSurvivesReenable(t *testing.T) {
	svc, repo := newTicketPolicySettings()
	cfg, err := svc.GetCodexTicketSettings(context.Background())
	require.NoError(t, err)
	p := config.DefaultCodexTicketProtection()
	p.Enabled = true
	cfg.Protection = &p
	_, err = svc.UpdateCodexTicketSettings(context.Background(), cfg)
	require.NoError(t, err)
	cfg.Protection.Enabled = false
	_, err = svc.UpdateCodexTicketSettings(context.Background(), cfg)
	require.NoError(t, err)
	watermark := repo.values[SettingKeyCodexTicketProtectionDisabledAt]
	require.NotEmpty(t, watermark)
	cfg.Protection.Enabled = true
	_, err = svc.UpdateCodexTicketSettings(context.Background(), cfg)
	require.NoError(t, err)
	require.Equal(t, watermark, repo.values[SettingKeyCodexTicketProtectionDisabledAt])
	disabled, err := svc.GetCodexTicketProtectionDisabledAt(context.Background())
	require.NoError(t, err)
	require.WithinDuration(t, time.Now(), disabled, time.Second)
}

func TestCodexTicketStrategyMergeAndExplicitWrite(t *testing.T) {
	current := map[string]any{CodexTicketProxyStrategyExtraKey: CodexTicketProxyStrategyRoundRobin}
	merged := MergeOpenAICodexTicketExtra(map[string]any{"name": "kept"}, current)
	require.Equal(t, CodexTicketProxyStrategyRoundRobin, merged[CodexTicketProxyStrategyExtraKey])
	for _, raw := range []any{"random", true, 1, ""} {
		require.Error(t, ValidateCodexTicketProxyExtra(map[string]any{CodexTicketProxyStrategyExtraKey: raw}))
	}
	ctx := WithCodexTicketProxyWrite(context.Background(), current)
	require.True(t, CodexTicketProxyStrategyWrite(ctx))
	require.False(t, CodexTicketProxyStrategyWrite(context.Background()))
	require.NotContains(t, codexTicketProxyExplicitExtra(merged, nil), CodexTicketProxyStrategyExtraKey)
}

func TestCodexTicketOutcomeCountsDoNotRebuildOldLifetimeTotals(t *testing.T) {
	history := CodexTicketHistory{Summary: CodexTicketAttemptSummary{Total: 500, Failed: 490, Success: 10}}
	start := time.Now()
	history.Append(CodexTicketAttempt{StartedAt: start, Outcome: "verification_deferred"})
	require.EqualValues(t, 501, history.Summary.Total)
	require.EqualValues(t, 491, history.Summary.Failed)
	require.Equal(t, map[string]int64{"verification_deferred": 1}, history.Summary.OutcomeCounts)
	require.Equal(t, start, *history.Summary.ClassificationStartedAt)
	filter, err := ParseCodexTicketHistoryFilter(map[string][]string{"outcome": {"verification_deferred"}})
	require.NoError(t, err)
	require.True(t, filter.matches(history.Items[0]))
	require.False(t, filter.matches(CodexTicketAttempt{Reason: "business_failed"}))
}

func TestCodexTicketRetryJitterNeverPrecedesHardDeadline(t *testing.T) {
	now := time.Now()
	for _, delay := range []time.Duration{time.Second, time.Minute, time.Hour} {
		until := now.Add(delay)
		for range 30 {
			got := codexTicketRetryWithJitter(until, now)
			require.False(t, got.Before(until))
			require.LessOrEqual(t, got.Sub(until), min(2*time.Second, delay/4))
		}
	}
}
