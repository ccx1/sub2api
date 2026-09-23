package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketBusinessVerificationSwitch(t *testing.T) {
	for _, mode := range []string{config.CodexTicketLengthStrict, config.CodexTicketLengthAuto} {
		for _, enabled := range []bool{true, false} {
			t.Run(mode+"/"+map[bool]string{true: "enabled", false: "disabled"}[enabled], func(t *testing.T) {
				upstream := &codexTicketVerificationUpstream{respond: func(int) *http.Response {
					return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(292))
				}}
				cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, LengthMode: mode,
					VerifyBusiness: &enabled, HarvestProxyURL: "http://harvest.example:8080"}
				svc, account := ticketTestService(t, cfg, upstream), ticketTestAccount(41)
				proxyID := int64(7)
				account.ProxyID = &proxyID
				account.Proxy = &Proxy{ID: proxyID, Protocol: "http", Host: "business.example", Port: 8080}
				svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
				calls := 1
				if enabled {
					calls = 2
				}
				require.Len(t, upstream.requests, calls)
				ticket := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
				require.NotNil(t, ticket)
				require.Equal(t, enabled, ticket.Verified)
				require.Equal(t, !enabled, ticket.VerificationSkipped)
				require.Equal(t, openAICodexTicketEgress("http://business.example:8080"), ticket.Egress)
				require.True(t, ticket.usable(time.Now(), account, svc.openAICodexTicketConfig()))
				if !enabled {
					on := true
					svc.cfg.Gateway.OpenAICodexTicket.VerifyBusiness = &on
					require.True(t, svc.openAICodexTicketBlocksAccount(account, "gpt-6-astra"))
				}
			})
		}
	}
}

func TestCodexTicketSkippingVerificationStillRejectsModelMismatch(t *testing.T) {
	enabled := false
	u := &codexTicketVerificationUpstream{respond: func(int) *http.Response {
		return codexTicketCompletedResponse("different-model", fakeCodexTicketState(292))
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, VerifyBusiness: &enabled, HarvestProxyURL: "http://harvest.example:8080"}, u)
	account := ticketTestAccount(41)
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Len(t, u.requests, 1)
	require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
}

func TestCodexTicketVerificationSwitchDiscardsInflightCandidate(t *testing.T) {
	for _, initial := range []bool{true, false} {
		next := !initial
		u := &codexTicketVerificationUpstream{}
		svc := ticketTestService(t, config.OpenAICodexTicketConfig{
			Enabled: true, VerifyBusiness: &initial, HarvestProxyURL: "http://harvest.example:8080",
		}, u)
		u.respond = func(int) *http.Response {
			svc.cfg.Gateway.OpenAICodexTicket.VerifyBusiness = &next
			return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(292))
		}
		account := ticketTestAccount(41)
		svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
		require.Len(t, u.requests, 1, "修改复核策略后不能继续旧采集流程")
		require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"), "旧策略候选票不能发布")
	}
}

func TestCodexTicketSkippingVerificationFinishesSharedSchedule(t *testing.T) {
	svc, repo := codexScheduledFixture(t, true)
	enabled := false
	svc.cfg.Gateway.OpenAICodexTicket.VerifyBusiness = &enabled
	calls := 0
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		require.Equal(t, 1, repo.validates, "首次发送前必须先校验共享租约")
		calls++
		return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(356)), nil
	}}
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Equal(t, 1, calls)
	require.Equal(t, 1, repo.started)
	require.Equal(t, 2, repo.validates, "关闭复核仍需在出站前和发布前分别校验共享租约")
	require.Zero(t, repo.stages)
	require.Len(t, repo.finishes, 1)
	require.Equal(t, "success", repo.finishes[0].Outcome)
	require.False(t, repo.finishes[0].BusinessProxySucceeded, "未探测的业务出口不能报告成功")
	require.Len(t, repo.history, 1)
	require.Equal(t, "verification_skipped", repo.history[0].Reason)
	require.Nil(t, repo.history[0].BusinessHTTPStatus)
	ticket := svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra")
	require.NotNil(t, ticket)
	require.True(t, ticket.VerificationSkipped)
}

func TestCodexTicketSkippingVerificationRejectsLostLease(t *testing.T) {
	for _, phase := range []string{"before_send", "before_publish"} {
		t.Run(phase, func(t *testing.T) {
			svc, base := codexScheduledFixture(t, true)
			enabled := false
			svc.cfg.Gateway.OpenAICodexTicket.VerifyBusiness = &enabled
			repo := &codexQualityLeaseRepo{codexScheduleRepo: base, lost: phase == "before_send"}
			svc.accountRepo = repo
			calls := 0
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				calls++
				repo.lost = true
				return codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(356)), nil
			}}
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			wantCalls := 0
			if phase == "before_publish" {
				wantCalls = 1
			}
			require.Equal(t, wantCalls, calls)
			require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
			require.Len(t, repo.finishes, 1)
			require.False(t, repo.finishes[0].HarvestProxyFailed)
			require.False(t, repo.finishes[0].BusinessProxySucceeded)
			if phase == "before_send" {
				require.Empty(t, repo.history, "未出站的任务不记录为已执行探测")
				return
			}
			require.Len(t, repo.history, 1)
			require.False(t, repo.history[0].Success)
			require.Equal(t, "controls_changed", repo.history[0].Reason)
		})
	}
}
