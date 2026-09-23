package service

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSharedPoolTicketSnapshotUsesEligibleStandby(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	account.Extra = map[string]any{}
	account.Status = StatusActive
	primary := &openAICodexTicket{State: fakeCodexTicketState(332), Length: 332, CapturedAt: now, ExpiresAt: now.Add(time.Hour)}
	primary.Standby = &openAICodexTicket{State: fakeCodexTicketState(292), Length: 292, CapturedAt: now, ExpiresAt: now.Add(time.Hour)}
	account.Extra[openAICodexTicketExtraKey("gpt-6-astra")] = primary
	cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"gpt-6-astra"}})
	snapshot := NewSharedPoolTicketAccountSnapshot(account, now)
	require.True(t, snapshot.hasReadyModelForConfig(cfg.Models, cfg, now), "主票套餐不符时备用仍应计入容量")
	primary.Revoked = true
	snapshot = NewSharedPoolTicketAccountSnapshot(account, now)
	require.True(t, snapshot.hasReadyModelForConfig(cfg.Models, cfg, now))
}

func TestSharedPoolTicketSnapshotHonorsVerificationPolicyAfterCapture(t *testing.T) {
	now := time.Now()
	account := ticketTestAccount(41)
	account.Extra = map[string]any{}
	account.Status = StatusActive
	account.Extra[openAICodexTicketExtraKey("gpt-6-astra")] = &openAICodexTicket{
		State: fakeCodexTicketState(292), Length: 292, CapturedAt: now, ExpiresAt: now.Add(time.Hour),
		VerificationSkipped: true, AccountBinding: openAICodexTicketAccountBinding(account),
	}
	snapshot := NewSharedPoolTicketAccountSnapshot(account, now)
	for _, mode := range []string{config.CodexTicketLengthStrict, config.CodexTicketLengthAuto} {
		for _, enabled := range []bool{false, true} {
			cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{
				Enabled: true, LengthMode: mode, VerifyBusiness: &enabled, Models: []string{"gpt-6-astra"},
			})
			require.Equal(t, !enabled, snapshot.hasReadyModelForConfig(cfg.Models, cfg, now))
		}
	}
}

func sharedPoolReserveFixture() (*Account, *openAICodexTicket) {
	account := sharedTicketProgressAccount()
	ticket := func() *openAICodexTicket {
		return &openAICodexTicket{State: fakeCodexTicketState(332), Length: 332,
			CapturedAt: sharedTicketProgressNow, ExpiresAt: sharedTicketProgressNow.Add(time.Hour)}
	}
	primary := ticket()
	primary.Standby = ticket()
	primary.Reserve = []*openAICodexTicket{ticket(), ticket(), ticket()}
	account.Extra[openAICodexTicketExtraKey("gpt-6-astra")] = primary
	return account, primary
}

func TestSharedPoolTicketSnapshotUsesEveryReserveSlot(t *testing.T) {
	for _, tc := range []struct {
		index       int
		unavailable bool
	}{{0, false}, {1, false}, {2, false}, {0, true}, {1, true}, {2, true}} {
		t.Run(fmt.Sprintf("reserve_%d_unavailable_%t", tc.index, tc.unavailable), func(t *testing.T) {
			account, primary := sharedPoolReserveFixture()
			eligible := primary.Reserve[tc.index]
			eligible.Length, eligible.State = 292, fakeCodexTicketState(292)
			eligible.ExpiresAt = sharedTicketProgressNow.Add(time.Minute)
			if tc.unavailable {
				primary.Revoked = true
				primary.Standby.ExpiresAt = sharedTicketProgressNow
			}
			capacity := sharedTicketProgressCapacity(t, account)
			cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: []string{"gpt-6-astra"}}
			snapshot := capacity.TicketAccounts[0]
			require.True(t, snapshot.hasReadyModel(cfg.Models, 292, sharedTicketProgressNow))
			requireSharedTicketAvailability(t, GetSharedPoolCatalogCapacity(capacity, cfg, sharedTicketProgressNow), true)
			requireSharedTicketAvailability(t, GetSharedPoolCatalogCapacity(capacity, cfg, eligible.ExpiresAt), false)
		})
	}
}

func TestSharedPoolTicketReserveHonorsLengthAndVerificationPolicies(t *testing.T) {
	account, primary := sharedPoolReserveFixture()
	eligible := primary.Reserve[1]
	eligible.Length, eligible.State = 292, fakeCodexTicketState(292)
	eligible.VerificationSkipped, eligible.AccountBinding = true, openAICodexTicketAccountBinding(account)
	snapshot := NewSharedPoolTicketAccountSnapshot(account, sharedTicketProgressNow)
	for _, mode := range []string{config.CodexTicketLengthStrict, config.CodexTicketLengthAuto} {
		for _, enabled := range []bool{false, true} {
			cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{
				Enabled: true, LengthMode: mode, VerifyBusiness: &enabled, Models: []string{"gpt-6-astra"},
			})
			require.Equal(t, !enabled, snapshot.hasReadyModelForConfig(cfg.Models, cfg, sharedTicketProgressNow),
				"mode=%s verification=%t", mode, enabled)
			if mode == config.CodexTicketLengthStrict {
				cfg.RejectedLengths = []int{292}
				require.False(t, snapshot.hasReadyModelForConfig(cfg.Models, cfg, sharedTicketProgressNow))
			}
		}
	}
}

func TestSharedPoolTicketReserveSnapshotIsDetachedAndPrivate(t *testing.T) {
	account, primary := sharedPoolReserveFixture()
	eligible := primary.Reserve[1]
	eligible.Length, eligible.State = 292, fakeCodexTicketState(292)
	before, err := json.Marshal(account)
	require.NoError(t, err)
	capacity := sharedTicketProgressCapacity(t, account)
	after, err := json.Marshal(account)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after))
	for _, value := range []any{capacity.TicketAccounts[0], capacity} {
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		for _, secret := range []string{eligible.State, "credential-sentinel", "access_token", "reserve"} {
			require.NotContains(t, string(encoded), secret)
			require.NotContains(t, fmt.Sprintf("%#v", value), secret)
		}
	}
	eligible.State, eligible.ExpiresAt = "changed", sharedTicketProgressNow.Add(-time.Minute)
	cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: []string{"gpt-6-astra"}}
	requireSharedTicketAvailability(t, GetSharedPoolCatalogCapacity(capacity, cfg, sharedTicketProgressNow), true)
	requireSharedTicketAvailability(t, GetSharedPoolCatalogCapacity(sharedTicketProgressCapacity(t, account), cfg, sharedTicketProgressNow), false)
}
