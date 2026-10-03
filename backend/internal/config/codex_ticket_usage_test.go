package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketUsageNormalizeDefaultsToImmediate(t *testing.T) {
	cases := []struct {
		name    string
		in      OpenAICodexTicketConfig
		mode    string
		minAge  int
		consume bool
	}{
		{name: "legacy", in: OpenAICodexTicketConfig{}, mode: CodexTicketUsageImmediate},
		{name: "unknown", in: OpenAICodexTicketConfig{UsageMode: "fifo", MinTicketAgeSeconds: 30}, mode: CodexTicketUsageImmediate},
		{name: "aged_without_age", in: OpenAICodexTicketConfig{UsageMode: CodexTicketUsageAged}, mode: CodexTicketUsageImmediate},
		{name: "immediate_drops_age", in: OpenAICodexTicketConfig{UsageMode: CodexTicketUsageImmediate, MinTicketAgeSeconds: 30}, mode: CodexTicketUsageImmediate},
		{name: "aged", in: OpenAICodexTicketConfig{UsageMode: CodexTicketUsageAged, MinTicketAgeSeconds: 300, ConsumeAfterUse: true}, mode: CodexTicketUsageAged, minAge: 300, consume: true},
		{name: "aged_capped", in: OpenAICodexTicketConfig{UsageMode: CodexTicketUsageAged, MinTicketAgeSeconds: MaxCodexTicketMinAgeSeconds + 1}, mode: CodexTicketUsageAged, minAge: DefaultCodexTicketHistoricalValiditySeconds - 1},
		{name: "consume_only", in: OpenAICodexTicketConfig{ConsumeAfterUse: true}, mode: CodexTicketUsageImmediate, consume: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := NormalizeOpenAICodexTicketConfig(tc.in)
			require.Equal(t, tc.mode, cfg.UsageMode)
			require.Equal(t, tc.minAge, cfg.MinTicketAgeSeconds)
			require.Equal(t, tc.consume, cfg.ConsumeAfterUse)
			require.Equal(t, tc.mode == CodexTicketUsageAged, CodexTicketUsageAgedEnabled(cfg))
		})
	}
}

func TestValidateCodexTicketUsageBoundsAgeByHistoricalValidity(t *testing.T) {
	cfg := OpenAICodexTicketConfig{TTLSeconds: 3600}
	require.NoError(t, ValidateCodexTicketUsage(&cfg))
	require.Equal(t, CodexTicketUsageImmediate, cfg.UsageMode)

	cfg = OpenAICodexTicketConfig{TTLSeconds: 3600, UsageMode: CodexTicketUsageImmediate, MinTicketAgeSeconds: 999999}
	require.NoError(t, ValidateCodexTicketUsage(&cfg))
	require.Zero(t, cfg.MinTicketAgeSeconds, "即取即用不保留沉淀时长")

	for _, age := range []int{1, 300, 3600, 7 * 86400} {
		cfg = OpenAICodexTicketConfig{TTLSeconds: 3600, UsageMode: CodexTicketUsageAged, MinTicketAgeSeconds: age}
		require.NoError(t, ValidateCodexTicketUsage(&cfg), age)
		require.Equal(t, age, cfg.MinTicketAgeSeconds)
	}
	for _, age := range []int{0, -1, DefaultCodexTicketHistoricalValiditySeconds} {
		cfg = OpenAICodexTicketConfig{TTLSeconds: 3600, UsageMode: CodexTicketUsageAged, MinTicketAgeSeconds: age}
		require.ErrorContains(t, ValidateCodexTicketUsage(&cfg), "min_ticket_age_seconds", age)
	}
	cfg = OpenAICodexTicketConfig{TTLSeconds: 3600, UsageMode: CodexTicketUsageAged, MinTicketAgeSeconds: MaxCodexTicketMinAgeSeconds - 1, HistoricalTicketValiditySeconds: MaxCodexTicketHistoricalValiditySeconds}
	require.NoError(t, ValidateCodexTicketUsage(&cfg))
	cfg.MinTicketAgeSeconds = MaxCodexTicketMinAgeSeconds + 1
	require.Error(t, ValidateCodexTicketUsage(&cfg))

	cfg = OpenAICodexTicketConfig{TTLSeconds: 3600, UsageMode: "oldest"}
	require.ErrorContains(t, ValidateCodexTicketUsage(&cfg), "取票机制")
}

func TestWithoutCodexTicketUsagePolicyOnlyStripsUsageFields(t *testing.T) {
	base := OpenAICodexTicketConfig{Enabled: true, TTLSeconds: 3600, TargetLength: 292, FailClosed: true}
	withPolicy := base
	withPolicy.UsageMode, withPolicy.MinTicketAgeSeconds, withPolicy.ConsumeAfterUse = CodexTicketUsageAged, 300, true
	require.Equal(t, base, WithoutCodexTicketUsagePolicy(withPolicy))
	require.Equal(t, CodexTicketUsageAged, withPolicy.UsageMode, "不修改调用方配置")
}

func TestCodexTicketUsageEnvironmentDefaultsAndValidation(t *testing.T) {
	resetViperWithJWTSecret(t)
	cfg, err := Load()
	require.NoError(t, err)
	ticket := cfg.Gateway.OpenAICodexTicket
	require.Equal(t, CodexTicketUsageImmediate, ticket.UsageMode)
	require.Equal(t, 8*86400, ticket.HistoricalTicketValiditySeconds)
	require.Zero(t, ticket.MinTicketAgeSeconds)
	require.False(t, ticket.ConsumeAfterUse)

	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_USAGE_MODE", CodexTicketUsageAged)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_MIN_TICKET_AGE_SECONDS", "300")
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_CONSUME_AFTER_USE", "true")
	cfg, err = Load()
	require.NoError(t, err)
	ticket = cfg.Gateway.OpenAICodexTicket
	require.Equal(t, CodexTicketUsageAged, ticket.UsageMode)
	require.Equal(t, 300, ticket.MinTicketAgeSeconds)
	require.True(t, ticket.ConsumeAfterUse)

	// 历史票的独立有效期必须严格大于沉淀时长。
	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_USAGE_MODE", CodexTicketUsageAged)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_MIN_TICKET_AGE_SECONDS", "691200")
	_, err = Load()
	require.ErrorContains(t, err, "min_ticket_age_seconds")

	// 即取即用会忽略遗留的沉淀时长，而不是拒绝启动。
	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_USAGE_MODE", CodexTicketUsageImmediate)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_MIN_TICKET_AGE_SECONDS", "3600")
	cfg, err = Load()
	require.NoError(t, err)
	require.Equal(t, CodexTicketUsageImmediate, cfg.Gateway.OpenAICodexTicket.UsageMode)
	require.Zero(t, cfg.Gateway.OpenAICodexTicket.MinTicketAgeSeconds)

	resetViperWithJWTSecret(t)
	t.Setenv("GATEWAY_OPENAI_CODEX_TICKET_USAGE_MODE", "newest")
	_, err = Load()
	require.ErrorContains(t, err, "取票机制")
}
