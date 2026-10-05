package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func historicalTestConfig(capacity int) config.OpenAICodexTicketConfig {
	return config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{
		Enabled: true, Models: []string{usageTestModel}, PoolCapacity: capacity,
		UsageMode: config.CodexTicketUsageAged, MinTicketAgeSeconds: 7 * 86400,
		HistoricalTicketValiditySeconds: 8 * 86400, FailClosed: true,
	})
}

func TestHistoricalTicketValidityStartsAfterFirstUse(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	cfg := historicalTestConfig(1)
	cfg.HistoricalTicketValiditySeconds = 300
	ticket := usageTestTicket("U", 24*time.Hour, now)

	require.Zero(t, ticket.historicalExpires(cfg))
	require.True(t, ticket.usable(now, account, cfg), "未使用的历史票不应因采集时间过期")

	ticket.HistoricalUsedAt = now
	require.WithinDuration(t, now.Add(300*time.Second), ticket.historicalExpires(cfg), time.Second)
	require.True(t, ticket.usable(now.Add(299*time.Second), account, cfg))
	require.False(t, ticket.usable(now.Add(301*time.Second), account, cfg))
}

func TestHistoricalTicketUsesSevenDayOldStateUntilIndependentDeadline(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	cfg := historicalTestConfig(2)
	young := usageTestTicket("Y", time.Hour, now)
	old := usageTestTicket("O", 7*24*time.Hour+time.Hour, now)
	young.HistoricalUsedAt, old.HistoricalUsedAt = now, now
	old.StateExpiresAt, old.ExpiresAt = now.Add(-6*24*time.Hour), now.Add(-6*24*time.Hour)
	pool := usageTestPool(young, old)
	require.False(t, old.valid(now, 292), "协议硬期限已过")
	selected := selectOpenAICodexTicket(pool, account, cfg, now)
	require.NotNil(t, selected)
	require.Equal(t, old.State, selected.State)
	status := codexTicketPoolStatus(usageTestModel, pool, account, cfg, now)
	require.True(t, status.Ready)
	require.Equal(t, "maturing", status.PrimaryReason)
	require.False(t, status.PrimaryReady)
	require.WithinDuration(t, now.Add(8*24*time.Hour), *status.PrimaryExpiresAt, time.Second)
	require.Greater(t, status.PrimaryRemainingSeconds, int64(7*24*time.Hour/time.Second))
	require.False(t, status.RevalidationRequired)
	require.WithinDuration(t, old.CapturedAt, *status.OriginCapturedAt, time.Second)
	require.WithinDuration(t, now.Add(8*24*time.Hour), *status.ExpiresAt, time.Second)
	require.InDelta(t, 8*24*3600, status.RemainingSeconds, 2)
	require.Nil(t, selectOpenAICodexTicket(pool, account, cfg, now.Add(8*24*time.Hour)))
	youngOnly := codexTicketPoolStatus(usageTestModel, usageTestPool(young), account, cfg, now)
	require.False(t, youngOnly.Ready)
	require.True(t, youngOnly.Blocked)
	require.Zero(t, youngOnly.AvailableCount)
	require.Zero(t, youngOnly.ReserveCount)
}

func TestHistoricalCookieFollowsTicketWindowOnOutboundRequest(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	cfg := historicalTestConfig(1)
	cfg.CredentialMode = config.CodexTicketCredentialCookie
	ticket := usageTestTicket("C", 7*24*time.Hour+time.Hour, now)
	ticket.HistoricalUsedAt = now
	ticket.State, ticket.Length, ticket.CredentialMode = "", 0, config.CodexTicketCredentialCookie
	ticket.Cookies = []*http.Cookie{{Name: "session", Value: "old", Domain: "chatgpt.com", Path: "/backend-api", Secure: true, Expires: now.Add(-time.Hour)}}
	selected := selectOpenAICodexTicket(ticket, account, cfg, now)
	require.NotNil(t, selected)
	u, err := url.Parse(chatgptCodexURL)
	require.NoError(t, err)
	require.Len(t, selected.rawCookiesForURL(u), 1)
	require.Equal(t, selected.historicalExpires(cfg), selected.rawCookiesForURL(u)[0].Expires)
	require.Empty(t, ticket.rawCookiesForURL(u), "库存原票仍保留原始 Cookie 元数据")
}

func TestHistoricalStatusReportsSelectedTicketAndRecentHarvestAttempt(t *testing.T) {
	now := time.Now()
	cfg := historicalTestConfig(2)
	account := ticketTestAccount(41)
	young := usageTestTicket("Y", time.Hour, now)
	old := usageTestTicket("O", 7*24*time.Hour+time.Hour, now)
	old.HistoricalUsedAt = now
	attempt := CodexTicketAttempt{Model: usageTestModel, StartedAt: now.Add(-time.Minute), Reason: "verified", Success: true}
	account.Extra = map[string]any{
		openAICodexTicketExtraKey(usageTestModel): usageTestPool(young, old),
		OpenAICodexTicketHistoryKey:               CodexTicketHistory{Items: []CodexTicketAttempt{attempt}},
	}
	status := OpenAICodexTicketStatuses(account, cfg, now)[0]
	require.Equal(t, config.CodexTicketUsageAged, status.UsageMode)
	require.True(t, status.Ready)
	require.WithinDuration(t, old.CapturedAt, *status.OriginCapturedAt, time.Second)
	require.WithinDuration(t, now.Add(8*24*time.Hour), *status.ExpiresAt, time.Second)
	require.WithinDuration(t, attempt.StartedAt, *status.LastAttemptAt, time.Second)
	require.True(t, *status.LastAttemptSuccess)
	require.Equal(t, "verified", status.LastAttemptReason)
}

func TestHistoricalFullPoolPreservesImmatureTicketsAndReplacesMatured(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	cfg := historicalTestConfig(2)
	first, second := usageTestTicket("A", 3*time.Hour, now), usageTestTicket("B", 2*time.Hour, now)
	incoming := usageTestTicket("C", 0, now)
	pool := usageTestPool(first, second)
	require.True(t, codexTicketPoolNeedsRefresh(pool, account, cfg, now), "满池仍定期打票")
	preserved := mergeCodexTicketPublication(pool, incoming, account, cfg)
	require.Equal(t, []string{first.State, second.State}, usageTestStates(codexTicketSlots(preserved)...))
	first.CapturedAt = now.Add(-7*24*time.Hour - time.Hour)
	pool = usageTestPool(first, second)
	replaced := mergeCodexTicketPublication(pool, incoming, account, cfg)
	require.Equal(t, []string{second.State, incoming.State}, usageTestStates(codexTicketSlots(replaced)...))
}

func thousandTicketPool(now time.Time) ([]*openAICodexTicket, config.OpenAICodexTicketConfig) {
	cfg := historicalTestConfig(5)
	cfg.AccountPoolCapacity = 1000
	tickets := make([]*openAICodexTicket, 1000)
	for index := range tickets {
		ticket := usageTestTicket("A", 7*24*time.Hour+time.Duration(index+1)*time.Second, now)
		ticket.State = openAICodexTicketStatePrefix + fmt.Sprintf("%06d", index) + ticket.State[12:]
		tickets[index] = ticket
	}
	return tickets, cfg
}

func TestHistoricalAccountPoolSupportsThousandTickets(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	tickets, cfg := thousandTicketPool(now)
	pool := composeCodexTicketPool(tickets, cfg)
	require.Len(t, codexTicketSlots(pool), 1000)
	encoded, err := json.Marshal(pool)
	require.NoError(t, err)
	parsed := parseOpenAICodexTicketFromAny(account.ID, usageTestModel, json.RawMessage(encoded))
	require.Len(t, codexTicketSlots(parsed), 1000)
	selected := selectOpenAICodexTicket(parsed, account, cfg, now)
	require.NotNil(t, selected)
	require.Equal(t, tickets[999].State, selected.State, "历史票最老优先")
	incoming := usageTestTicket("N", 0, now)
	replaced := mergeCodexTicketPublication(parsed, incoming, account, cfg)
	require.Len(t, codexTicketSlots(replaced), 1000)
	require.True(t, codexTicketInventoryContains(replaced, incoming))
}

func BenchmarkHistoricalThousandTicketPool(b *testing.B) {
	now := time.Now()
	account := ticketTestAccount(41)
	tickets, cfg := thousandTicketPool(now)
	pool := composeCodexTicketPool(tickets, cfg)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		encoded, err := json.Marshal(pool)
		if err != nil {
			b.Fatal(err)
		}
		parsed := parseOpenAICodexTicketFromAny(account.ID, usageTestModel, json.RawMessage(encoded))
		if selectOpenAICodexTicket(parsed, account, cfg, now) == nil {
			b.Fatal("missing historical ticket")
		}
	}
}
