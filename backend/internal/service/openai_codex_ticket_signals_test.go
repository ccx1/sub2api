package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCodexTicketSignalsWhitelistAndZeroValues(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Codex-Safety-Buffering-Enabled", "false")
	headers.Set("X-Codex-Primary-Used-Percent", "0")
	headers.Set("X-Codex-Primary-Reset-At", "1790000000")
	headers.Set("Authorization", "never-copy-me")
	s := codexTicketSignalsFromHeaders(headers)
	require.NotNil(t, s)
	require.NotNil(t, s.SafetyBufferingEnabled)
	require.False(t, *s.SafetyBufferingEnabled)
	require.Len(t, s.Quota, 1)
	require.Equal(t, float64(0), *s.Quota[0].UsedPercent)
	encoded, err := json.Marshal(s)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "never-copy-me")
	require.Contains(t, string(encoded), `"reset_at":"2026-`)
	metadata := gjson.Parse(`{"type":"codex.response.metadata","headers":{"X-Codex-Plan-Type":"team","Cookie":"secret"}}`)
	s = observeCodexTicketSignals(s, metadata, "codex.response.metadata")
	require.Equal(t, "team", s.PlanType)
	require.Len(t, s.Quota, 1)
}

func TestCodexTicketSignalsBoundsAndAdditionalTruncation(t *testing.T) {
	additional := make([]any, 0, 9)
	for i := 0; i < 9; i++ {
		additional = append(additional, map[string]any{"limit_name": string(rune('a' + i)), "rate_limit": map[string]any{"primary": map[string]any{"used_percent": 50}}})
	}
	data, err := json.Marshal(map[string]any{"additional_rate_limits": additional})
	require.NoError(t, err)
	s := observeCodexTicketSignals(nil, gjson.ParseBytes(data), "codex.rate_limits")
	require.Len(t, s.Quota, 8)
	require.True(t, s.Truncated)
	headers := http.Header{}
	headers.Set("X-Codex-Safety-Buffering-Faster-Model", strings.Repeat("中", 100))
	headers.Set("X-Codex-Plan-Type", "bad\x00")
	headers.Set("X-Codex-Primary-Used-Percent", "NaN")
	headers.Set("X-Codex-Secondary-Reset-At", "1e99")
	s = codexTicketSignalsFromHeaders(headers)
	require.True(t, s.Truncated)
	require.LessOrEqual(t, len(s.FasterModel), 160)
	require.Empty(t, s.PlanType)
	require.Empty(t, s.Quota)
	require.Nil(t, observeCodexTicketSignals(nil, gjson.Parse(`{"plan_type":"private"}`), "unknown.event"))
}

func TestCodexTicketSignalsHTTPWSKnownAliasesAndNamespaces(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Codex-Spark-Primary-Used-Percent", "40")
	headers.Set("X-Codex-Spark-Primary-Limit-Reached", "false")
	headers.Set("X-Codex-Primary-Used-Percent", "25")
	httpSignals := codexTicketSignalsFromHeaders(headers)
	wsSignals := observeCodexTicketSignals(nil, gjson.Parse(`{"planType":"pro","meteredLimitName":"premium","rateLimit":{"primary":{"usedPercent":25}},"additionalRateLimits":[{"limitName":"spark","rateLimit":{"limitReached":false,"primary":{"usedPercent":40}}}]}`), "codex.rate_limits")
	require.Equal(t, httpSignals.Quota, wsSignals.Quota)
	require.Equal(t, "pro", wsSignals.PlanType)
	require.Equal(t, "premium", wsSignals.ActiveLimit)
	errorSignals := observeCodexTicketSignals(nil, gjson.Parse(`{"headers":{"X-Codex-Primary-Used-Percent":"100","Cookie":"secret","X-Codex-Turn-State":"secret"}}`), "error")
	require.Len(t, errorSignals.Quota, 1)
	require.Equal(t, float64(100), *errorSignals.Quota[0].UsedPercent)
	encoded, err := json.Marshal(errorSignals)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret")
}
