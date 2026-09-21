package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketTierRulesMatchAllRuntimePaths(t *testing.T) {
	for _, tier := range []string{"team", "pro"} {
		for _, length := range []int{292, 332} {
			t.Run(fmt.Sprintf("%s_%d", tier, length), func(t *testing.T) {
				cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, Models: []string{"gpt-6-astra"}, HarvestProxyURL: "http://harvest.example:8080"}
				account := ticketTestAccount(41)
				account.Status, account.Credentials["plan_type"] = StatusActive, tier
				calls := 0
				svc := ticketTestService(t, cfg, &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
					calls++
					return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(length)), nil
				}})
				svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
				want := tier == "team" && length == 332 || tier == "pro" && length == 292
				require.Equal(t, want, svc.lookupOpenAICodexTicket(account, "gpt-6-astra") != nil)
				if want {
					require.Equal(t, 2, calls)
				} else {
					require.Equal(t, 1, calls)
				}
				// 加载旧票，再验证所有消费侧都执行同一规则。
				ticket := &openAICodexTicket{Model: "gpt-6-astra", State: fakeCodexTicketState(length), Length: length, CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}
				account.Extra = map[string]any{openAICodexTicketExtraKey(ticket.Model): ticket}
				svc = ticketTestService(t, cfg, nil)
				req, _ := http.NewRequest(http.MethodPost, "http://localhost", nil)
				require.Equal(t, want, svc.applyOpenAICodexTicketRequest(account, ticket.Model, req) == nil)
				require.Equal(t, !want, svc.openAICodexTicketBlocksAccount(account, ticket.Model))
				require.Equal(t, want, OpenAICodexTicketStatuses(account, cfg, time.Now())[0].Ready)
				ws := openAIWSAcquireRequest{}
				require.Equal(t, want, svc.refreshOpenAICodexTicketWSHeaders(context.Background(), account, ticket.Model, &ws) == nil)
				require.Equal(t, want, ws.CodexTicketReceipt != nil)
				snapshot := NewSharedPoolTicketAccountSnapshot(account, time.Now())
				snapshot.Available, snapshot.Concurrency = true, 3
				capacity := &SharedPoolCapacity{AvailableAccounts: 1, ConcurrencyCapacity: 3, TicketAccounts: []SharedPoolTicketAccountSnapshot{*snapshot}}
				require.Equal(t, want, GetSharedPoolCatalogCapacity(capacity, cfg, time.Now()).AvailableAccounts == 1)
			})
		}
	}
}

func TestCodexTicketTierIdentificationUsesExactExplicitRules(t *testing.T) {
	cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{TargetLength: 280})
	cfg.TierRules = append(cfg.TierRules, config.CodexTicketTierRule{Tier: "self_serve_business_prolite", Aliases: []string{"business_premium"}, TargetLength: 352})
	for _, tc := range []struct {
		plan, lower string
		length      int
	}{
		{" Team ", "pro", 332}, {"ChatGPT-Pro", "team", 292}, {"business_standard", "", 332},
		{"self_serve_business_prolite", "pro", 352}, {"business premium", "pro", 352},
		{"prolite", "pro", 280}, {"custom_pro_plus", "pro", 280}, {"", "team", 332},
	} {
		account := ticketTestAccount(41)
		account.Credentials["plan_type"], account.Credentials["chatgpt_plan_type"] = tc.plan, tc.lower
		require.Equal(t, tc.length, openAICodexTicketTargetLength(account, cfg), tc.plan)
	}
}

func TestCodexTicketTierJWTRequiresMatchingAccount(t *testing.T) {
	account := ticketTestAccount(41)
	jwt := func(id, tier string) string {
		payload := fmt.Sprintf(`{"https://api.openai.com/auth":{"chatgpt_account_id":%q,"chatgpt_plan_type":%q}}`, id, tier)
		return "header." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".signature"
	}
	account.Credentials["id_token"] = jwt("other", "pro")
	account.Credentials["access_token"] = jwt("acc-1", "team")
	require.Equal(t, "team", openAICodexTicketSubscriptionTier(account))
	account.Credentials["plan_type"] = "future"
	require.Equal(t, "future", openAICodexTicketSubscriptionTier(account))
}

func TestCodexTicketPolicyChangesStopSecondPhaseAndPublication(t *testing.T) {
	for _, phase := range []int{1, 2} {
		t.Run(fmt.Sprint(phase), func(t *testing.T) {
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://harvest.example:8080"}, nil)
			calls := 0
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				calls++
				if calls == phase {
					svc.cfg.Gateway.OpenAICodexTicket.TargetLength = 332
				}
				return codexTicketResponse(), nil
			}}
			account := ticketTestAccount(41)
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.Equal(t, phase, calls)
			require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
			require.False(t, svc.openAICodexTicketBackoffActive(account, "tok", "gpt-6-astra"))
		})
	}
}

func TestCodexTicketDynamicRulesRetainLegalOldTicket(t *testing.T) {
	svc, account, receipt := codexTicketWSFixture(t)
	account.Status, account.Credentials["plan_type"] = StatusActive, "pro"
	account.Extra = map[string]any{openAICodexTicketExtraKey(receipt.ticket.Model): receipt.ticket, ProxyModeExtraKey: ProxyModeRandom}
	cfg := config.NormalizeOpenAICodexTicketConfig(svc.cfg.Gateway.OpenAICodexTicket)
	cfg.RetryBackoffSeconds, cfg.HarvestProbeIntervalSeconds = []int{90, 180}, 30
	svc.cfg.Gateway.OpenAICodexTicket = cfg
	callCount := 0
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) { callCount++; return codexTicketResponse(), nil }}
	svc.probeOnceOpenAICodexTicket(context.Background(), account, receipt.ticket.Model)
	require.Zero(t, callCount, "合法随机旧票仍优先使用")
	require.False(t, svc.openAICodexTicketBlocksAccount(account, receipt.ticket.Model))
	snapshot := NewSharedPoolTicketAccountSnapshot(account, time.Now())
	snapshot.Available, snapshot.Concurrency = true, 3
	capacity := &SharedPoolCapacity{AvailableAccounts: 1, ConcurrencyCapacity: 3, TicketAccounts: []SharedPoolTicketAccountSnapshot{*snapshot}}
	cfg.TierRules = []config.CodexTicketTierRule{{Tier: "pro", TargetLength: 352}}
	svc.cfg.Gateway.OpenAICodexTicket = cfg
	require.True(t, svc.openAICodexTicketBlocksAccount(account, receipt.ticket.Model))
	require.Zero(t, GetSharedPoolCatalogCapacity(capacity, cfg, time.Now()).AvailableAccounts)
	require.NotNil(t, svc.lookupOpenAICodexTicket(account, receipt.ticket.Model), "规则更新不删除旧票")
}
