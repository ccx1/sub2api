package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func codexTicketControlDailyCooldown() map[string]any {
	now := time.Now().UTC()
	return map[string]any{"enabled": true, "timezone": "UTC",
		"start": now.Add(-time.Hour).Format("15:04"), "end": now.Add(time.Hour).Format("15:04")}
}

func TestCodexTicketControlGlobalDisableStopsBusinessProbe(t *testing.T) {
	repo := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
		SettingKeyOpenAICodexTicketEnabled: "true", SettingKeyOpenAICodexTicketHarvestProxyMode: "fixed",
		SettingKeyOpenAICodexTicketHarvestProxyURL: "http://harvest.example:8080",
	}}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
	svc.settingService = NewSettingService(repo, svc.cfg)
	calls := 0
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			repo.values[SettingKeyOpenAICodexTicketEnabled] = "false"
			svc.settingService.InvalidateOpenAICodexTicketEnabledCache()
		}
		return codexTicketResponse(), nil
	}}
	svc.probeOnceOpenAICodexTicket(context.Background(), ticketTestAccount(41), "gpt-6-astra")
	require.Equal(t, 1, calls, "关闭后不能再启动业务出口复验")
}

func TestCodexTicketControlBusinessProbeHonorsDailyCooldown(t *testing.T) {
	calls := 0
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, &codexTicketFuncUpstream{
		do: func(*http.Request) (*http.Response, error) { calls++; return codexTicketResponse(), nil },
	})
	account := ticketTestAccount(41)
	account.Extra = map[string]any{DailyCooldownExtraKey: codexTicketControlDailyCooldown()}
	require.True(t, account.IsInDailyCooldown(time.Now()))
	input := openAICodexTicketProbeInput{Account: account, Token: "tok", Model: "gpt-6-astra", Timeout: time.Second}
	require.False(t, svc.verifyOpenAICodexTicketBusiness(context.Background(), &input, fakeCodexTicketState(292)))
	require.Zero(t, calls)
}

type codexTicketControlRepo struct {
	AccountRepository
	account *Account
	err     error
	reads   int
}

func (r *codexTicketControlRepo) GetCodexTicketAccountSnapshot(context.Context, int64) (*Account, error) {
	r.reads++
	if r.account == nil {
		return nil, r.err
	}
	return cloneOpenAICodexTicketAccount(r.account), r.err
}

func (*codexTicketControlRepo) UpdateExtra(context.Context, int64, map[string]any) error { return nil }

func codexTicketControlService(t *testing.T) (*OpenAIGatewayService, *codexTicketControlRepo) {
	t.Helper()
	account := ticketTestAccount(41)
	account.Status, account.Extra = StatusActive, map[string]any{}
	repo := &codexTicketControlRepo{account: account}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://harvest.example:8080"}, nil)
	svc.accountRepo = repo
	return svc, repo
}

func TestCodexTicketControlReloadsBeforeBusinessProbe(t *testing.T) {
	changes := map[string]func(*codexTicketControlRepo){
		"account_disabled": func(r *codexTicketControlRepo) { r.account.Extra[OpenAICodexTicketEnabledExtraKey] = false },
		"daily_cooldown": func(r *codexTicketControlRepo) {
			r.account.Extra[DailyCooldownExtraKey] = codexTicketControlDailyCooldown()
		},
		"tls_changed":     func(r *codexTicketControlRepo) { r.account.Extra["tls_fingerprint_builtin"] = "nodejs24" },
		"token_changed":   func(r *codexTicketControlRepo) { r.account.Credentials["access_token"] = "rotated" },
		"refresh_changed": func(r *codexTicketControlRepo) { r.account.Credentials["refresh_token"] = "rotated" },
		"inactive":        func(r *codexTicketControlRepo) { r.account.Status = StatusDisabled },
		"deleted":         func(r *codexTicketControlRepo) { r.account = nil },
		"read_error":      func(r *codexTicketControlRepo) { r.err = errors.New("snapshot unavailable") },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			svc, repo := codexTicketControlService(t)
			account := cloneOpenAICodexTicketAccount(repo.account)
			calls := 0
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				calls++
				if calls == 1 {
					change(repo)
				}
				return codexTicketResponse(), nil
			}}
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.Equal(t, 1, calls)
			require.Equal(t, 3, repo.reads, "采集开始、发送前与业务复验前检查最新快照")
			require.Nil(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra"))
		})
	}
}

func TestCodexTicketControlReloadsBeforeFirstProbe(t *testing.T) {
	svc, repo := codexTicketControlService(t)
	account := cloneOpenAICodexTicketAccount(repo.account)
	repo.account.Extra[OpenAICodexTicketEnabledExtraKey] = false
	calls := 0
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls++
		return codexTicketResponse(), nil
	}}
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Zero(t, calls)
	require.Equal(t, 1, repo.reads)
}

func TestCodexTicketControlCancellationStopsBusinessProbe(t *testing.T) {
	svc, repo := codexTicketControlService(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls++
		cancel()
		return codexTicketResponse(), nil
	}}
	svc.probeOnceOpenAICodexTicket(ctx, repo.account, "gpt-6-astra")
	require.Equal(t, 1, calls)
}

func TestCodexTicketControlUnchangedAccountCompletesBothPhases(t *testing.T) {
	svc, repo := codexTicketControlService(t)
	calls := 0
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		calls++
		return codexTicketResponse(), nil
	}}
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Equal(t, 2, calls)
	require.Equal(t, 5, repo.reads, "采集、复验、各次发送与发布前分别检查最新快照")
	require.True(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra").valid(time.Now(), 292))
}

func TestCodexTicketControlRechecksImmediatelyBeforeSend(t *testing.T) {
	for _, stop := range []string{"global", "account", "daily", "rejected", "canceled"} {
		t.Run(stop, func(t *testing.T) {
			svc, repo := codexTicketControlService(t)
			calls := 0
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				calls++
				return codexTicketResponse(), nil
			}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			input := openAICodexTicketProbeInput{Account: repo.account, Token: "tok", Model: "gpt-6-astra", CheckControls: true}
			req, err := svc.buildOpenAICodexTicketProbeRequest(ctx, input)
			require.NoError(t, err)
			switch stop {
			case "global":
				svc.cfg.Gateway.OpenAICodexTicket.Enabled = false
			case "account":
				repo.account.Extra[OpenAICodexTicketEnabledExtraKey] = false
			case "daily":
				repo.account.Extra[DailyCooldownExtraKey] = codexTicketControlDailyCooldown()
			case "rejected":
				svc.coolOpenAICodexTicket(repo.account, "tok", &openAICodexTicketProbeRejected{Status: http.StatusTooManyRequests})
			case "canceled":
				cancel()
			}
			_, err = svc.doOpenAICodexTicketProbe(req, input)
			require.Error(t, err)
			require.Zero(t, calls)
			require.Zero(t, repo.reads, "发送前检查复用同一快照")
		})
	}
}
