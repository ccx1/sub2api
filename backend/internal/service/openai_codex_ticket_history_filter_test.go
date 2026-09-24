package service

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func ticketHistoryLifecycleFixture(t *testing.T) (*Account, time.Time) {
	t.Helper()
	base := time.Now().Add(-2 * time.Hour).UTC()
	expired, future := base.Add(time.Hour), time.Now().Add(time.Hour).UTC()
	history := CodexTicketHistory{}
	for _, item := range []CodexTicketAttempt{
		{ID: "legacy", StartedAt: base, Model: "gpt-5.6-sol", Success: true, Reason: "verified"},
		{ID: "failed", StartedAt: base.Add(time.Minute), Model: "gpt-6-astra", Reason: "harvest_model_mismatch"},
		{ID: "expired", StartedAt: base.Add(2 * time.Minute), Model: "gpt-6-astra", Success: true, Reason: "verified", TicketCapturedAt: &base, TicketExpiresAt: &expired},
		{ID: "revoked", StartedAt: base.Add(3 * time.Minute), Model: "gpt-6-astra", Success: true, Reason: "verified", TicketCapturedAt: &base, TicketExpiresAt: &expired},
		{ID: "issued", StartedAt: base.Add(4 * time.Minute), Model: "gpt-6-astra", Success: true, Reason: "verified", TicketCapturedAt: &base, TicketExpiresAt: &future},
	} {
		history.Append(item)
	}
	account := ticketTestAccount(41)
	account.Extra = map[string]any{
		OpenAICodexTicketHistoryKey: history,
		OpenAICodexTicketInvalidationsKey: []CodexTicketInvalidation{{
			AttemptID: "revoked", Model: "gpt-6-astra", CapturedAt: base, InvalidatedAt: base.Add(8 * time.Minute),
			Reason: "response_model_mismatch", Source: "http", ReportedModels: []string{"gpt-5.6-luna"},
		}},
	}
	return account, base
}

func TestCodexTicketHistoryProjectsRevocationBeforeTTLWithoutChangingAttempts(t *testing.T) {
	account, base := ticketHistoryLifecycleFixture(t)
	history, err := GetCodexTicketHistory(account, 1, 20)
	require.NoError(t, err)
	require.EqualValues(t, 5, history.Summary.Total)
	require.EqualValues(t, 4, history.Summary.Success)
	require.EqualValues(t, 1, history.Summary.Failed)
	require.Equal(t, []string{"issued", "invalidated", "ttl_elapsed", "not_issued", "unknown"}, []string{
		history.Items[0].TicketStatus, history.Items[1].TicketStatus, history.Items[2].TicketStatus,
		history.Items[3].TicketStatus, history.Items[4].TicketStatus,
	})
	revoked := history.Items[1]
	require.True(t, revoked.Success)
	require.Equal(t, "verified", revoked.Reason)
	require.Equal(t, "response_model_mismatch", revoked.Invalidation.Reason)
	require.True(t, base.Add(8*time.Minute).Equal(revoked.Invalidation.InvalidatedAt))
	require.Equal(t, []string{"gpt-5.6-luna"}, revoked.Invalidation.ReportedModels)
	require.Nil(t, history.Items[2].Invalidation, "TTL is inferred from the recorded expiry, not a fabricated revocation")
	require.Nil(t, history.Items[4].Invalidation)
	require.Equal(t, []string{"gpt-5.6-sol", "gpt-6-astra"}, history.FilterOptions.Models)
	require.Equal(t, []string{"attempt:harvest_model_mismatch", "invalidation:response_model_mismatch", "invalidation:ttl_expired"}, history.FilterOptions.Reasons)
	stored, err := DecodeCodexTicketHistory(account.Extra[OpenAICodexTicketHistoryKey])
	require.NoError(t, err)
	require.Empty(t, stored.Items[1].TicketStatus)
	require.Nil(t, stored.Items[1].Invalidation)
}

func TestCodexTicketHistoryPrunesRecordsOutsideTwelveHours(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	history := CodexTicketHistory{Items: []CodexTicketAttempt{
		{ID: "future", StartedAt: now.Add(time.Minute)},
		{ID: "boundary", StartedAt: now.Add(-OpenAICodexTicketHistoryRetention)},
		{ID: "recent", StartedAt: now.Add(-11 * time.Hour)},
		{ID: "expired", StartedAt: now.Add(-OpenAICodexTicketHistoryRetention - time.Nanosecond)},
		{ID: "missing-time"},
	}}

	history.PruneCodexTicketHistory(now)

	require.Equal(t, []string{"future", "recent", "boundary"}, []string{history.Items[0].ID, history.Items[1].ID, history.Items[2].ID})
}

func TestGetCodexTicketHistoryHidesLegacyRecordsOutsideTwelveHours(t *testing.T) {
	now := time.Now().UTC()
	history := CodexTicketHistory{}
	history.Append(CodexTicketAttempt{ID: "expired", StartedAt: now.Add(-13 * time.Hour)})
	history.Append(CodexTicketAttempt{ID: "recent", StartedAt: now.Add(-11 * time.Hour)})
	account := ticketTestAccount(41)
	account.Extra = map[string]any{OpenAICodexTicketHistoryKey: history}

	got, err := GetCodexTicketHistory(account, 1, 20)

	require.NoError(t, err)
	require.Equal(t, []string{"recent"}, []string{got.Items[0].ID})
	require.Equal(t, 1, got.Total)
	require.EqualValues(t, 2, got.Summary.Total)
}

func TestCodexTicketHistoryFiltersBeforePaginationAndPreservesOptions(t *testing.T) {
	account, base := ticketHistoryLifecycleFixture(t)
	from, to := base.Add(2*time.Minute), base.Add(4*time.Minute)
	filter := CodexTicketHistoryFilter{Result: "success", Model: "gpt-6-astra", StartedFrom: &from, StartedTo: &to}
	history, err := GetCodexTicketHistory(account, 2, 2, filter)
	require.NoError(t, err)
	require.Equal(t, 3, history.Total)
	require.Len(t, history.Items, 1)
	require.Equal(t, "expired", history.Items[0].ID)
	require.EqualValues(t, 5, history.Summary.Total)
	require.Len(t, history.FilterOptions.Models, 2)
	require.Len(t, history.FilterOptions.Reasons, 3)
	history, err = GetCodexTicketHistory(account, int(^uint(0)>>1), 100, filter)
	require.NoError(t, err)
	require.Empty(t, history.Items)
	require.Equal(t, 3, history.Total)
	filter.TicketStatus, filter.Reason = "invalidated", "invalidation:response_model_mismatch"
	history, err = GetCodexTicketHistory(account, 1, 20, filter)
	require.NoError(t, err)
	require.Len(t, history.Items, 1)
	require.Equal(t, "revoked", history.Items[0].ID)
}

func TestCodexTicketHistoryFiltersFailureAndTTLReasons(t *testing.T) {
	account, _ := ticketHistoryLifecycleFixture(t)
	for _, fixture := range []struct {
		filter CodexTicketHistoryFilter
		id     string
	}{
		{CodexTicketHistoryFilter{Result: "failed", Reason: "attempt:harvest_model_mismatch"}, "failed"},
		{CodexTicketHistoryFilter{TicketStatus: "ttl_elapsed", Reason: "invalidation:ttl_expired"}, "expired"},
		{CodexTicketHistoryFilter{TicketStatus: "unknown"}, "legacy"},
	} {
		history, err := GetCodexTicketHistory(account, 1, 20, fixture.filter)
		require.NoError(t, err)
		require.Len(t, history.Items, 1)
		require.Equal(t, fixture.id, history.Items[0].ID)
	}
}

func TestCodexTicketHistoryInvalidationRequiresExactTicketIdentity(t *testing.T) {
	account, base := ticketHistoryLifecycleFixture(t)
	valid := CodexTicketInvalidation{AttemptID: "issued", Model: "gpt-6-astra", CapturedAt: base,
		InvalidatedAt: base.Add(time.Minute), Reason: "response_model_mismatch", Source: "http"}
	for name, mutate := range map[string]func(*CodexTicketInvalidation){
		"other attempt":  func(e *CodexTicketInvalidation) { e.AttemptID = "other" },
		"other model":    func(e *CodexTicketInvalidation) { e.Model = "gpt-5.6-sol" },
		"other capture":  func(e *CodexTicketInvalidation) { e.CapturedAt = base.Add(time.Second) },
		"no time":        func(e *CodexTicketInvalidation) { e.InvalidatedAt = time.Time{} },
		"before capture": func(e *CodexTicketInvalidation) { e.InvalidatedAt = base.Add(-time.Second) },
	} {
		t.Run(name, func(t *testing.T) {
			event := valid
			mutate(&event)
			account.Extra[OpenAICodexTicketInvalidationsKey] = []CodexTicketInvalidation{event}
			history, err := GetCodexTicketHistory(account, 1, 20)
			require.NoError(t, err)
			require.Equal(t, "issued", history.Items[0].TicketStatus)
			require.Nil(t, history.Items[0].Invalidation)
		})
	}
}

func TestCodexTicketHistoryRejectsMalformedInvalidationLedger(t *testing.T) {
	account, _ := ticketHistoryLifecycleFixture(t)
	account.Extra[OpenAICodexTicketInvalidationsKey] = "not-a-ledger"
	_, err := GetCodexTicketHistory(account, 1, 20)
	require.Error(t, err)
}

func TestCodexTicketHistoryProjectsMatchingRevokedTombstoneOnly(t *testing.T) {
	account, base := ticketHistoryLifecycleFixture(t)
	delete(account.Extra, OpenAICodexTicketInvalidationsKey)
	event := CodexTicketInvalidation{AttemptID: "issued", Model: "gpt-6-astra", CapturedAt: base,
		InvalidatedAt: base.Add(time.Minute), Reason: "response_model_mismatch", Source: "http"}
	ticket := openAICodexTicket{AttemptID: event.AttemptID, Model: event.Model, CapturedAt: base, Revoked: true, Invalidation: &event}
	account.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
	history, err := GetCodexTicketHistory(account, 1, 20)
	require.NoError(t, err)
	require.Equal(t, "invalidated", history.Items[0].TicketStatus)
	require.Equal(t, "issued", history.Items[0].Invalidation.AttemptID)
	ticket.AttemptID = "newer-ticket"
	account.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
	history, err = GetCodexTicketHistory(account, 1, 20)
	require.NoError(t, err)
	require.Equal(t, "issued", history.Items[0].TicketStatus)
	require.Nil(t, history.Items[0].Invalidation)
}

func TestParseCodexTicketHistoryFilterValidatesAndTrims(t *testing.T) {
	query := url.Values{"result": {" success "}, "ticket_status": {" invalidated "}, "model": {" gpt-6-astra "},
		"reason": {" invalidation:response_model_mismatch "}, "started_from": {"2026-09-21T00:00:00Z"}, "started_to": {"2026-09-21T08:00:00+08:00"}}
	filter, err := ParseCodexTicketHistoryFilter(query)
	require.NoError(t, err)
	require.Equal(t, "success", filter.Result)
	require.Equal(t, "invalidated", filter.TicketStatus)
	require.Equal(t, "gpt-6-astra", filter.Model)
	require.Equal(t, "invalidation:response_model_mismatch", filter.Reason)
	require.True(t, filter.StartedFrom.Equal(*filter.StartedTo), "date boundaries are inclusive and timezone aware")
	for _, invalid := range []url.Values{
		{"result": {"maybe"}}, {"ticket_status": {"active"}}, {"model": {strings.Repeat("a", 257)}},
		{"reason": {"model_mismatch"}}, {"reason": {"attempt:"}}, {"reason": {"unknown:reason"}},
		{"reason": {"attempt:bad reason"}}, {"reason": {"invalidation:" + strings.Repeat("a", 160)}},
		{"started_from": {"2026-09-21"}}, {"started_to": {"not a date"}},
		{"started_from": {"2026-09-22T00:00:00Z"}, "started_to": {"2026-09-21T00:00:00Z"}},
	} {
		_, err := ParseCodexTicketHistoryFilter(invalid)
		require.Error(t, err, invalid)
	}
}
