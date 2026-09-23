package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func codexTicketDisableCases() map[string]func(*Account) {
	return map[string]func(*Account){
		"inactive":       func(a *Account) { a.Status = "inactive" },
		"scheduling_off": func(a *Account) { a.Schedulable = false },
		"sharing_off": func(a *Account) {
			a.Extra[SharedPoolOwnerKey], a.Extra[SharedPoolEnabledKey] = int64(7), false
		},
		"admin_disabled": func(a *Account) {
			a.Extra[SharedPoolOwnerKey], a.Extra[SharedPoolEnabledKey], a.Extra[SharedPoolAdminDisabledKey] = int64(7), true, true
		},
		"sharing_not_enabled": func(a *Account) { a.Extra[SharedPoolOwnerKey] = int64(7) },
	}
}

func TestCodexTicketDisabledAccountStopsEveryProbeStage(t *testing.T) {
	for name, disable := range codexTicketDisableCases() {
		for _, manual := range []bool{false, true} {
			for _, stage := range []string{"entry", "queued", "business", "publish"} {
				t.Run(name+"/"+map[bool]string{false: "auto", true: "manual"}[manual]+"/"+stage, func(t *testing.T) {
					svc, repo := codexTicketControlService(t)
					repo.account.Schedulable = true
					original := cloneOpenAICodexTicketAccount(repo.account)
					calls, want := 0, 0
					if stage == "business" {
						want = 1
					}
					if stage == "publish" {
						want = 2
					}
					if want == 0 {
						disable(repo.account)
					}
					snapshot := original
					if stage == "entry" {
						snapshot = cloneOpenAICodexTicketAccount(repo.account)
					}
					svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
						calls++
						if calls == want {
							disable(repo.account)
						}
						return codexTicketResponse(), nil
					}}
					ctx := context.Background()
					if manual {
						ctx = withCodexTicketManualRetry(ctx)
					}
					svc.probeOnceOpenAICodexTicket(ctx, snapshot, "gpt-6-astra")
					require.Equal(t, want, calls)
					require.Nil(t, svc.lookupOpenAICodexTicket(original, "gpt-6-astra"), "禁用后不得发布新票")
					require.True(t, OpenAICodexTicketAccountEnabled(repo.account), "暂停账号不能改写打票开关")
				})
			}
		}
	}
}

func TestCodexTicketDisabledAccountsSkippedByBothRefreshLoops(t *testing.T) {
	for name, disable := range codexTicketDisableCases() {
		for _, scheduled := range []bool{false, true} {
			t.Run(name+"/"+map[bool]string{false: "legacy", true: "scheduled"}[scheduled], func(t *testing.T) {
				a := ticketTestAccount(41)
				a.Status, a.Schedulable, a.Extra = StatusActive, true, map[string]any{}
				disable(a)
				calls := 0
				svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, Models: []string{"gpt-6-astra"}, HarvestProxyURL: "http://harvest.example:8080"},
					&codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) { calls++; return codexTicketResponse(), nil }})
				if scheduled {
					repo := &codexScheduleRepo{codexTicketControlRepo: &codexTicketControlRepo{account: a}}
					svc.accountRepo = repo
					svc.refreshScheduledCodexTickets(context.Background(), []Account{*a}, svc.openAICodexTicketConfig())
					require.Zero(t, repo.started)
					require.Empty(t, repo.history)
				} else {
					repo := &codexTicketRefreshRepo{accounts: []Account{*a}}
					svc.accountRepo = repo
					svc.refreshOpenAICodexTickets(context.Background())
					require.Empty(t, repo.updates)
				}
				require.Zero(t, calls)
			})
		}
	}
}

func TestCodexTicketDisabledQueueCannotRunStaleAutomaticOrManualTasks(t *testing.T) {
	svc, repo := codexTicketControlService(t)
	repo.account.Schedulable = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entered, release := make(chan struct{}, 4), make(chan struct{})
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return codexTicketResponse(), nil
	}}
	first := svc.submitCodexTicketProbes(ctx, repo.account, []string{"gpt-6-astra"}, false)
	awaitCodexTicketProbeSignals(t, entered, 1)
	automatic := svc.submitCodexTicketProbes(ctx, repo.account, []string{"gpt-6-astra"}, false)
	manual := svc.submitCodexTicketProbes(withCodexTicketManualRetry(ctx), repo.account, []string{"gpt-6-astra"}, true)
	repo.account.Extra[SharedPoolOwnerKey], repo.account.Extra[SharedPoolEnabledKey] = int64(7), false
	close(release)
	for _, task := range append(append(first, automatic...), manual...) {
		awaitCodexTicketProbeSignals(t, task.done, 1)
	}
	require.Empty(t, entered, "账号关闭后，已排队任务也不能再发出请求")
	require.Zero(t, svc.openaiCodexTicketManualPending.Load())
	require.Zero(t, svc.openaiCodexTicketActive.Load())
}

func TestCodexTicketHarvestResumesWhenAccountEnabledDespiteTemporaryDispatchHolds(t *testing.T) {
	svc, repo := codexTicketControlService(t)
	repo.account.Schedulable = false
	until := time.Now().Add(time.Hour)
	repo.account.TempUnschedulableUntil, repo.account.RateLimitResetAt, repo.account.OverloadUntil = &until, &until, &until
	calls := 0
	svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) { calls++; return codexTicketResponse(), nil }}
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Zero(t, calls)
	repo.account.Schedulable = true
	require.False(t, repo.account.IsSchedulable(), "临时业务准入暂停独立于人工启用")
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Equal(t, 2, calls)
	require.NotNil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
}

type codexTicketDisableOnAdmissionRepo struct {
	*codexScheduleRepo
	disable func(*Account)
}

func (r *codexTicketDisableOnAdmissionRepo) StartCodexTicket(ctx context.Context, reservation *CodexTicketReservation) error {
	r.disable(r.account)
	return r.codexScheduleRepo.StartCodexTicket(ctx, reservation)
}

func TestCodexTicketDisabledDuringAdmissionStopsBeforeNetworkSend(t *testing.T) {
	for name, disable := range codexTicketDisableCases() {
		t.Run(name, func(t *testing.T) {
			svc, base := codexScheduledFixture(t, false)
			base.account.Schedulable = true
			repo := &codexTicketDisableOnAdmissionRepo{codexScheduleRepo: base, disable: disable}
			svc.accountRepo = repo
			calls := 0
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) { calls++; return codexTicketResponse(), nil }}
			svc.probeOnceOpenAICodexTicket(withCodexTicketManualRetry(context.Background()), repo.account, "gpt-6-astra")
			require.Equal(t, 1, repo.started)
			require.Zero(t, calls, "获得资源后必须检查禁用状态才能发送请求")
			require.Len(t, repo.finishes, 1, "停止任务必须释放已获得的调度租约")
			require.NotEqual(t, "success", repo.finishes[0].Outcome)
		})
	}
}

func TestCodexTicketDisabledAccountRejectsManualRetry(t *testing.T) {
	for name, disable := range codexTicketDisableCases() {
		t.Run(name, func(t *testing.T) {
			a := ticketTestAccount(41)
			a.Extra = map[string]any{}
			disable(a)
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
			svc.accountRepo = &codexTicketRetryRepo{account: a}
			result, err := svc.RetryOpenAICodexTicket(context.Background(), a.ID, "")
			require.ErrorIs(t, err, ErrCodexTicketRetryUnavailable)
			require.Zero(t, result.Scheduled)
			require.Zero(t, svc.openaiCodexTicketManualPending.Load())
		})
	}
}
