package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketQualityRechecksAccountBeforeEveryExit(t *testing.T) {
	changes := map[string]func(*codexScheduleRepo){
		"refresh_token": func(repo *codexScheduleRepo) {
			repo.account.Credentials["refresh_token"] = "rotated-test-refresh"
		},
		"business_proxy": func(repo *codexScheduleRepo) {
			proxy := &Proxy{ID: 10, Status: StatusActive, Protocol: "http", Host: "changed.example", Port: 8080}
			repo.account.ProxyID, repo.account.Proxy = &proxy.ID, proxy
		},
		"harvest_policy": func(repo *codexScheduleRepo) {
			repo.account.Extra[CodexTicketProxyModeExtraKey] = CodexTicketProxyModeAccount
		},
		"deleted":        func(repo *codexScheduleRepo) { repo.account = nil },
		"snapshot_error": func(repo *codexScheduleRepo) { repo.err = errors.New("snapshot unavailable") },
	}
	for name, change := range changes {
		for _, afterCall := range []int{2, 4} {
			t.Run(fmt.Sprintf("%s/after_%d", name, afterCall), func(t *testing.T) {
				assertCodexQualityStopsAfterAccountChange(t, change, afterCall)
			})
		}
	}
}

func assertCodexQualityStopsAfterAccountChange(t *testing.T, change func(*codexScheduleRepo), afterCall int) {
	t.Helper()
	svc, repo := codexQualityFixture(t, config.CodexTicketCredentialCookieState)
	business := &Proxy{ID: 9, Status: StatusActive, Protocol: "http", Host: "business.example", Port: 8080}
	repo.account.ProxyID, repo.account.Proxy = &business.ID, business
	account := cloneOpenAICodexTicketAccount(repo.account)
	upstream := &codexTicketVerificationUpstream{respond: func(call int) *http.Response {
		if call == afterCall {
			change(repo)
		}
		return codexQualityResponse(call, "gpt-6-astra")
	}}
	svc.httpUpstream = upstream
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Len(t, upstream.requests, afterCall, "账号变化后不能继续质量轮次或独立业务出口请求")
	require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
	require.Len(t, repo.history, 1)
	require.Equal(t, "controls_changed", repo.history[0].Reason)
	require.Len(t, repo.finishes, 1)
	finish := repo.finishes[0]
	require.False(t, finish.QualityProxyFailed)
	require.False(t, finish.HarvestProxyFailed)
	require.False(t, finish.BusinessProxyFailed)
	require.False(t, finish.BusinessProxySucceeded)
	require.Zero(t, repo.businessFailures)
}

func TestCodexTicketQualityStopsWhenCredentialOverrideIsRemoved(t *testing.T) {
	for _, change := range []struct {
		name       string
		globalMode string
		policy     map[string]any
	}{
		{"inherit_mode", config.CodexTicketCredentialState, map[string]any{"mode": "inherit"}},
		{"delete_policy", config.CodexTicketCredentialState, nil},
		{"remove_ttl", config.CodexTicketCredentialCookieState, map[string]any{"mode": "cookie_state", "refresh_before_seconds": 5}},
		{"remove_refresh", config.CodexTicketCredentialCookieState, map[string]any{"mode": "cookie_state", "ttl_seconds": 45}},
	} {
		for _, afterCall := range []int{1, 2} {
			t.Run(fmt.Sprintf("%s/after_%d", change.name, afterCall), func(t *testing.T) {
				svc, repo := codexQualityFixture(t, change.globalMode)
				globalRefresh := 10
				svc.cfg.Gateway.OpenAICodexTicket.CookieRefreshBeforeSeconds = &globalRefresh
				repo.account.Extra[CodexTicketCredentialPolicyExtraKey] = map[string]any{
					"mode": "cookie_state", "ttl_seconds": 45, "refresh_before_seconds": 5,
				}
				upstream := &codexTicketVerificationUpstream{respond: func(call int) *http.Response {
					if call == afterCall {
						if change.policy == nil {
							delete(repo.account.Extra, CodexTicketCredentialPolicyExtraKey)
						} else {
							repo.account.Extra[CodexTicketCredentialPolicyExtraKey] = change.policy
						}
					}
					return codexQualityResponse(call, "gpt-6-astra")
				}}
				svc.httpUpstream = upstream
				svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
				require.Len(t, upstream.requests, afterCall, "撤销账号覆盖后必须按全局策略重新核验，不能沿用已解析的旧配置")
				require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
				require.Len(t, repo.history, 1)
				require.Equal(t, "controls_changed", repo.history[0].Reason)
				require.Len(t, repo.finishes, 1)
				require.False(t, repo.finishes[0].QualityProxyFailed)
				require.False(t, repo.finishes[0].BusinessProxyFailed)
			})
		}
	}
}
