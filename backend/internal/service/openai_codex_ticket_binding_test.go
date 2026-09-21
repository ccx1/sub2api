package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketConfigurationChangesHaveConsistentReadiness(t *testing.T) {
	for _, change := range []string{"fixed_proxy", "tls"} {
		t.Run(change, func(t *testing.T) {
			svc, account, previous, _ := ticketWatchdogFixture(t)
			account.Status = StatusActive
			account.Extra = map[string]any{}
			ticket := *previous
			ticket.Verified, ticket.Binding = true, svc.codexTicketBinding(account)
			ticket.AccountBinding = openAICodexTicketAccountBinding(account)
			ticket.Egress = openAICodexTicketEgress("")
			require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, &ticket))
			account.Extra[openAICodexTicketExtraKey(ticket.Model)] = &ticket
			check := func(ready bool) {
				t.Helper()
				require.Equal(t, ready, svc.lookupOpenAICodexTicket(account, ticket.Model).valid(time.Now(), 292))
				statuses := OpenAICodexTicketStatuses(account, svc.openAICodexTicketConfig(), time.Now())
				require.Equal(t, ready, statuses[0].Ready)
				snapshot := NewSharedPoolTicketAccountSnapshot(account, time.Now())
				require.NotNil(t, snapshot)
				require.Equal(t, ready, snapshot.hasReadyModel([]string{ticket.Model}, 292, time.Now()))
			}
			check(true)
			if change == "tls" {
				account.Extra["tls_fingerprint_builtin"] = "nodejs24"
			} else {
				id := int64(8)
				account.ProxyID, account.Proxy = &id, &Proxy{ID: 8, Protocol: "http", Host: "next.example", Port: 8080}
			}
			check(false)
			next := ticket
			next.CapturedAt = time.Now().Add(time.Second)
			next.Binding, next.Egress = svc.codexTicketBinding(account), openAICodexTicketEgress(resolveAccountProxyURL(account))
			require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, &next))
			account.Extra[openAICodexTicketExtraKey(ticket.Model)] = svc.lookupOpenAICodexTicket(account, ticket.Model)
			check(true)
		})
	}
}

func TestCodexTicketBindingIgnoresObservationMetadata(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	account := ticketTestAccount(41)
	account.Extra = map[string]any{ProxyModeExtraKey: ProxyModeRandom}
	initial := svc.codexTicketBinding(account)
	account.Extra["random_proxy_last_used"] = time.Now().Format(time.RFC3339Nano)
	account.Extra["codex_turn_ticket:other-model"] = "new ticket"
	account.Credentials["access_token"] = "refreshed token"
	require.Equal(t, initial, svc.codexTicketBinding(account))
	account.Extra[RandomProxyPoolIDsExtraKey] = []int64{1, 2}
	require.NotEqual(t, initial, svc.codexTicketBinding(account))
}

func TestCodexTicketLegacyEgressReadinessMatchesRuntime(t *testing.T) {
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	account := ticketTestAccount(41)
	account.Status = StatusActive
	legacy := &openAICodexTicket{Verified: true, Model: "gpt-6-astra", State: fakeCodexTicketState(292), Length: 292,
		CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), Egress: openAICodexTicketEgress("")}
	account.Extra = map[string]any{openAICodexTicketExtraKey(legacy.Model): legacy}
	for _, moved := range []bool{false, true} {
		if moved {
			id := int64(8)
			account.ProxyID, account.Proxy = &id, &Proxy{ID: id, Protocol: "http", Host: "other.example", Port: 8080}
		}
		headers := http.Header{}
		err := svc.applyOpenAICodexTicket(context.Background(), account, legacy.Model, headers)
		require.Equal(t, !moved, err == nil)
		require.Equal(t, !moved, OpenAICodexTicketStatuses(account, svc.openAICodexTicketConfig(), time.Now())[0].Ready)
		require.Equal(t, !moved, NewSharedPoolTicketAccountSnapshot(account, time.Now()).hasReadyModel([]string{legacy.Model}, 292, time.Now()))
	}
}

func TestCodexTicketRandomProxyGateDoesNotGuessDirectEgress(t *testing.T) {
	svc, account, previous, _ := ticketWatchdogFixture(t)
	account.Extra = map[string]any{ProxyModeExtraKey: ProxyModeRandom}
	verified := *previous
	verified.CapturedAt = time.Now()
	verified.Verified, verified.Binding = true, svc.codexTicketBinding(account)
	verified.Egress = openAICodexTicketEgress("http://verified.example:8080")
	require.True(t, svc.storeOpenAICodexTicket(context.Background(), account, &verified))
	// 调度尚未解析随机出口，不能把空关联当直连而拦截该账号。
	require.False(t, svc.openAICodexTicketBlocksAccount(account, verified.Model))
	proxyID := int64(7)
	account.ProxyID = &proxyID
	account.Proxy = &Proxy{ID: 7, Protocol: "http", Host: "verified.example", Port: 8080}
	headers := http.Header{}
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, verified.Model, headers))
	require.Equal(t, verified.State, headers.Get(openAICodexTurnStateHeader))
	account.Proxy = &Proxy{ID: 8, Protocol: "http", Host: "rotated.example", Port: 8080}
	headers = http.Header{}
	require.NoError(t, svc.applyOpenAICodexTicket(context.Background(), account, verified.Model, headers))
	require.Equal(t, verified.State, headers.Get(openAICodexTurnStateHeader))
	require.Equal(t, proxyID, *account.ProxyID)
}

type codexTicketCASStub struct {
	AccountRepository
	update func(*Account, string, any) (bool, error)
}

func (r *codexTicketCASStub) CompareAndSwapCodexTicket(_ context.Context, account *Account, model string, value any) (bool, error) {
	return r.update(account, model, value)
}

func TestCodexTicketCASConflictKeepsExistingMemory(t *testing.T) {
	svc, account, previous, _ := ticketWatchdogFixture(t)
	calls := 0
	svc.accountRepo = &codexTicketCASStub{update: func(*Account, string, any) (bool, error) {
		calls++
		return false, nil
	}}
	next := *previous
	next.CapturedAt = time.Now()
	require.False(t, svc.storeOpenAICodexTicket(context.Background(), account, &next))
	require.Equal(t, previous.CapturedAt, svc.lookupOpenAICodexTicket(account, previous.Model).CapturedAt)
	require.Equal(t, 1, calls)
}

func TestCodexTicketBindingHydratesSchedulingMetadataWithoutSelectingProxy(t *testing.T) {
	for _, random := range []bool{false, true} {
		t.Run(map[bool]string{false: "fixed", true: "random"}[random], func(t *testing.T) {
			svc, account, previous, _ := ticketWatchdogFixture(t)
			account.Extra = map[string]any{"tls_fingerprint_builtin": "nodejs24"}
			if random {
				account.Extra[ProxyModeExtraKey] = ProxyModeRandom
			} else {
				proxyID := int64(7)
				account.ProxyID = &proxyID
				account.Proxy = &Proxy{ID: 7, Protocol: "http", Host: "business.example", Port: 8080}
			}
			verified := *previous
			verified.Verified, verified.Binding = true, svc.codexTicketBinding(account)
			verified.AccountBinding = openAICodexTicketAccountBinding(account)
			verified.Egress = openAICodexTicketEgress("http://business.example:8080")
			account.Extra[openAICodexTicketExtraKey(previous.Model)] = &verified
			// 新进程没有内存票，仍应读取完整快照中的持久票据。
			cold := ticketTestService(t, svc.cfg.Gateway.OpenAICodexTicket, nil)
			cold.schedulerSnapshot = &SchedulerSnapshotService{cache: &openAISnapshotCacheStub{accountsByID: map[int64]*Account{account.ID: account}}}
			metadata := &Account{ID: account.ID, Platform: account.Platform, Type: account.Type}
			require.False(t, cold.openAICodexTicketBlocksAccount(metadata, previous.Model))
			// 完整账号的配置变化必须继续阻止旧票，不能以 metadata 不完整为由跳过绑定。
			account.Extra["tls_fingerprint_builtin"] = "changed-profile"
			require.True(t, cold.openAICodexTicketBlocksAccount(metadata, previous.Model))
			if random {
				require.Nil(t, account.ProxyID)
			}
		})
	}
}
