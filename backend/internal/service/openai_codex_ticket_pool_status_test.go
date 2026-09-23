package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketPoolCredentialStates(t *testing.T) {
	now := time.Now()
	for _, state := range []string{"available", "revalidation_required", "expired", "revoked", "missing"} {
		t.Run(state, func(t *testing.T) {
			_, account, ticket, _ := ticketWatchdogFixture(t)
			ticket.ExpiresAt, ticket.RevalidateAt = now.Add(time.Hour), now.Add(time.Minute)
			switch state {
			case "revalidation_required":
				ticket.RevalidateAt = now.Add(-time.Second)
			case "expired":
				ticket.ExpiresAt = now
			case "revoked":
				ticket.Revoked = true
			case "missing":
				ticket = nil
			}
			cfg := config.OpenAICodexTicketConfig{Enabled: true, PoolCapacity: 1, FailClosed: true}
			status := codexTicketPoolStatus("gpt-6-astra", ticket, account, cfg, now)
			ready := state == "available" || state == "revalidation_required"
			require.Equal(t, state, status.CredentialState)
			require.Equal(t, ready, status.Ready)
			require.Equal(t, !ready, status.Blocked)
			require.Equal(t, state == "revalidation_required", status.RevalidationRequired)
			if ready {
				require.Equal(t, 1, status.AvailableCount)
				require.True(t, status.RevalidateAt.Equal(ticket.RevalidateAt))
			} else {
				require.Zero(t, status.AvailableCount)
			}
			encoded, err := json.Marshal(status)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `"credential_state":"`+state+`"`)
			require.NotContains(t, string(encoded), `"state":`)
		})
	}
}

func TestCodexTicketPoolCredentialStateFollowsActiveStandby(t *testing.T) {
	now := time.Now()
	_, account, primary, _ := ticketWatchdogFixture(t)
	standby := inventoryTestTicket(primary, "S", time.Second)
	standby.RevalidateAt = now.Add(-time.Second)
	primary.Revoked, primary.Standby = true, standby
	status := codexTicketPoolStatus(primary.Model, primary, account, config.OpenAICodexTicketConfig{PoolCapacity: 2}, now)
	require.True(t, status.Ready)
	require.True(t, status.UsingStandby)
	require.Equal(t, "revoked", status.PrimaryReason)
	require.Equal(t, "revalidation_required", status.CredentialState)
	require.True(t, status.RevalidationRequired)
	require.Equal(t, 1, status.AvailableCount)
}

func TestCodexTicketPoolStatusAndRefreshDoNotMutateInventory(t *testing.T) {
	now := time.Now()
	_, account, primary, _ := ticketWatchdogFixture(t)
	primary.CapturedAt, primary.RevalidateAt = now.Add(-2*time.Minute), time.Time{}
	primary.Standby = inventoryTestTicket(primary, "S", time.Second)
	cfg := config.OpenAICodexTicketConfig{PoolCapacity: 2, TTLSeconds: 60, RetryBackoffSeconds: []int{1}}
	before := cloneCodexTicketInventory(primary)
	status := codexTicketPoolStatus(primary.Model, primary, account, cfg, now)
	require.Equal(t, "revalidation_required", status.CredentialState)
	require.Equal(t, 2, status.AvailableCount)
	require.True(t, status.RevalidateAt.Equal(primary.CapturedAt.Add(time.Minute)))
	require.True(t, codexTicketPoolNeedsRefresh(primary, account, cfg, now))
	require.Equal(t, before, primary, "read-only pool checks must preserve every shared snapshot")
}

func TestCodexTicketPoolStatusMalformedStateKeepsConfiguredExpiry(t *testing.T) {
	now := time.Now()
	_, account, ticket, _ := ticketWatchdogFixture(t)
	ticket.ExpiresAt, ticket.RevalidateAt = now.Add(time.Minute), now.Add(-time.Second)
	state := ticket.State
	parsed := parseOpenAICodexTicketFromAny(account.ID, ticket.Model, ticket)
	require.NotNil(t, parsed)
	require.Zero(t, parsed.StateExpiresAt)
	status := codexTicketPoolStatus(ticket.Model, parsed, account, config.OpenAICodexTicketConfig{PoolCapacity: 1}, now)
	require.Equal(t, "revalidation_required", status.CredentialState)
	require.Equal(t, 1, status.AvailableCount)
	require.True(t, status.ExpiresAt.Equal(ticket.ExpiresAt))
	require.Equal(t, state, ticket.State)
	status = codexTicketPoolStatus(ticket.Model, parsed, account, config.OpenAICodexTicketConfig{PoolCapacity: 1}, ticket.ExpiresAt)
	require.Equal(t, "expired", status.CredentialState)
	require.False(t, status.Ready)
}

func TestCodexTicketPoolRefreshIncludesPrimarySoftDeadline(t *testing.T) {
	now := time.Now()
	_, account, primary, _ := ticketWatchdogFixture(t)
	primary.RevalidateAt = now.Add(-time.Second)
	primary.Standby = inventoryTestTicket(primary, "S", time.Second)
	primary.Standby.RevalidateAt = now.Add(time.Hour)
	cfg := config.OpenAICodexTicketConfig{PoolCapacity: 2, TTLSeconds: 60, RetryBackoffSeconds: []int{1}}
	require.True(t, codexTicketPoolNeedsRefresh(primary, account, cfg, now))
	primary.RevalidateAt = now.Add(time.Hour)
	require.False(t, codexTicketPoolNeedsRefresh(primary, account, cfg, now))
}

func TestCodexTicketPoolSoftDeadlineUsesLastSuccessfulRevalidation(t *testing.T) {
	now := time.Now()
	_, account, primary, _ := ticketWatchdogFixture(t)
	primary.CapturedAt, primary.RevalidatedAt, primary.RevalidateAt = now.Add(-time.Hour), now, time.Time{}
	cfg := config.OpenAICodexTicketConfig{PoolCapacity: 1, TTLSeconds: 60, RetryBackoffSeconds: []int{1}}
	status := codexTicketPoolStatus(primary.Model, primary, account, cfg, now)
	require.Equal(t, "available", status.CredentialState)
	require.True(t, status.RevalidateAt.Equal(now.Add(time.Minute)))
	require.Zero(t, primary.RevalidateAt)
}
