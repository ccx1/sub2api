package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type accountEgressTicketRepo struct {
	accountTicketProxyRepo
	proxies     map[int64]*Proxy
	groupIDs    []int64
	disabledIDs []int64
}

func (r *accountEgressTicketRepo) GetCodexTicketProxy(_ context.Context, id int64) (*Proxy, error) {
	return r.proxies[id], r.proxyErr
}

func (r *accountEgressTicketRepo) SelectRandomActiveProxy(context.Context) (*Proxy, error) {
	panic("account egress must reuse the balanced proxy pool")
}

func (r *accountEgressTicketRepo) GetRandomProxyGroupIDs(context.Context, int64) ([]int64, error) {
	return r.groupIDs, nil
}

func (r *accountEgressTicketRepo) DisableRandomProxyAccountIfUnavailable(_ context.Context, id int64) error {
	r.disabledIDs = append(r.disabledIDs, id)
	r.account.Status, r.account.Schedulable = StatusDisabled, false
	return nil
}

func accountEgressTicketFixture(t *testing.T) (*OpenAIGatewayService, *accountEgressTicketRepo) {
	t.Helper()
	svc, base := accountTicketProxyFixture(t, CodexTicketProxyModeAccount)
	repo := &accountEgressTicketRepo{accountTicketProxyRepo: *base, proxies: map[int64]*Proxy{base.proxy.ID: base.proxy}}
	svc.accountRepo = repo
	return svc, repo
}

func TestCodexTicketAccountModeUsesFixedAccountExitAndKeepsUsableTicket(t *testing.T) {
	svc, repo := accountEgressTicketFixture(t)
	repo.account.ProxyID, repo.account.Proxy = &repo.proxy.ID, repo.proxy
	upstream := &ticketPoolUpstream{}
	svc.httpUpstream = upstream
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Equal(t, []string{repo.proxy.URL(), repo.proxy.URL()}, upstream.proxies)
	require.Empty(t, repo.selections)
	ticket := svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra")
	require.True(t, ticket.valid(time.Now(), 292))
	require.True(t, svc.storeOpenAICodexTicket(context.Background(), repo.account, inventoryTestTicket(ticket, "D", time.Second)))
	svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = "http://other-global.example:8080"
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Len(t, upstream.proxies, 2)
	require.Equal(t, ticket, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
}

func TestCodexTicketAccountModeExplicitlyFollowsDirect(t *testing.T) {
	svc, repo := accountEgressTicketFixture(t)
	upstream := &ticketPoolUpstream{}
	svc.httpUpstream = upstream
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Equal(t, []string{"", ""}, upstream.proxies)
	require.Empty(t, repo.selections)
	ticket := svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra")
	require.True(t, ticket.valid(time.Now(), 292))
	require.True(t, svc.storeOpenAICodexTicket(context.Background(), repo.account, inventoryTestTicket(ticket, "D", time.Second)))
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Len(t, upstream.proxies, 2)
	require.Equal(t, ticket, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
}

func TestCodexTicketGlobalAccountKeepsBusinessPolicySnapshot(t *testing.T) {
	svc, repo := accountEgressTicketFixture(t)
	repo.account.Extra[CodexTicketProxyModeExtraKey] = CodexTicketProxyModeInherit
	repo.account.ProxyID, repo.account.Proxy = &repo.proxy.ID, repo.proxy
	settings := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
		SettingKeyOpenAICodexTicketHarvestProxyMode: OpenAICodexTicketHarvestProxyModeAccount,
	}}}
	svc.settingService = NewSettingService(settings, svc.cfg)

	proxy, err := svc.selectOpenAICodexTicketProxy(context.Background(), repo.account)
	require.NoError(t, err)
	require.Equal(t, repo.proxy.ID, proxy.proxyID)
	require.True(t, proxy.policy.followBusiness)
	require.Equal(t, proxy.proxyID, proxy.policy.proxyID)
	require.Equal(t, proxy.url, proxy.policy.url)
	require.True(t, svc.codexTicketProxyPolicyCurrent(context.Background(), openAICodexTicketProbeInput{
		Account: repo.account, HarvestProxyPolicy: &proxy.policy,
	}))

	upstream := &ticketPoolUpstream{}
	svc.httpUpstream = upstream
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Equal(t, []string{repo.proxy.URL(), repo.proxy.URL()}, upstream.proxies)
	require.True(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra").valid(time.Now(), 292))
}

func TestCodexTicketAccountModeFollowsPersistedExpiryFallback(t *testing.T) {
	for _, mode := range []string{FallbackModeProxy, FallbackModeDirect} {
		t.Run(mode, func(t *testing.T) {
			svc, repo := accountEgressTicketFixture(t)
			backup := &Proxy{ID: 8, Status: StatusActive, Protocol: "http", Host: "backup.example", Port: 8080}
			repo.proxies[backup.ID] = backup
			expired := time.Now().Add(-time.Minute)
			repo.proxy.ExpiresAt, repo.proxy.FallbackMode, repo.proxy.BackupProxyID = &expired, mode, &backup.ID
			repo.account.ProxyID, repo.account.Proxy = &repo.proxy.ID, repo.proxy
			_, err := svc.selectOpenAICodexTicketProxy(context.Background(), repo.account)
			require.ErrorIs(t, err, ErrRandomProxyUnavailable)
			require.Equal(t, repo.proxy.ID, *repo.account.ProxyID)
			// 模拟既有 sweep 完成持久化改投，采集本身不能提前改写账号出口。
			target, changed := ResolveProxyFallbackTarget(*repo.proxy, map[int64]Proxy{backup.ID: *backup}, time.Now())
			require.True(t, changed)
			repo.account.ProxyID, repo.account.Proxy = target, nil
			wantURL := ""
			if target != nil {
				repo.account.Proxy, wantURL = repo.proxies[*target], backup.URL()
			}
			upstream := &ticketPoolUpstream{}
			svc.httpUpstream = upstream
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			require.Equal(t, []string{wantURL, wantURL}, upstream.proxies)
			require.Empty(t, repo.selections)
			require.True(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra").valid(time.Now(), 292))
		})
	}
}

func TestCodexTicketAccountModeFollowsEveryRandomScopeWithoutPersistingExit(t *testing.T) {
	for _, scope := range []string{RandomProxyPoolAll, RandomProxyPoolSelected, RandomProxyPoolGroup} {
		t.Run(scope, func(t *testing.T) {
			svc, repo := accountEgressTicketFixture(t)
			repo.account.Extra[ProxyModeExtraKey], repo.account.Extra[RandomProxyPoolScopeExtraKey] = ProxyModeRandom, scope
			repo.account.Extra[RandomProxyPoolIDsExtraKey] = []int64{7}
			repo.account.Extra[RandomProxyGroupIDExtraKey], repo.groupIDs = int64(3), []int64{7}
			upstream := &ticketPoolUpstream{}
			svc.httpUpstream = upstream
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			require.Equal(t, []string{repo.proxy.URL(), repo.proxy.URL()}, upstream.proxies)
			require.NotEmpty(t, repo.selections)
			for _, selection := range repo.selections {
				require.Equal(t, repo.account.ID, selection.AccountID)
				require.Equal(t, scope != RandomProxyPoolAll, selection.Restricted)
				if selection.Restricted {
					require.Equal(t, []int64{7}, selection.IDs)
				}
			}
			require.Nil(t, repo.account.ProxyID)
			require.True(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra").valid(time.Now(), 292))
		})
	}
}

func TestCodexTicketAccountModeHonorsEmptyGroupPolicies(t *testing.T) {
	for _, policy := range []string{RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDisable, RandomProxyEmptyPoolPolicyDirect} {
		t.Run(policy, func(t *testing.T) {
			svc, repo := accountEgressTicketFixture(t)
			repo.account.Extra[ProxyModeExtraKey] = ProxyModeRandom
			repo.account.Extra[RandomProxyPoolScopeExtraKey], repo.account.Extra[RandomProxyGroupIDExtraKey] = RandomProxyPoolGroup, int64(3)
			repo.account.Extra[RandomProxyEmptyPoolPolicyExtraKey] = policy
			upstream := &ticketPoolUpstream{}
			svc.httpUpstream = upstream
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			if policy == RandomProxyEmptyPoolPolicyDirect {
				require.Equal(t, []string{"", ""}, upstream.proxies)
				require.NotNil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
			} else {
				require.Empty(t, upstream.proxies)
				require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
			}
			require.Equal(t, policy == RandomProxyEmptyPoolPolicyDisable, len(repo.disabledIDs) > 0)
			for _, selection := range repo.selections {
				require.True(t, selection.Restricted)
				require.Empty(t, selection.IDs)
			}
		})
	}
}

func TestCodexTicketAccountModePoolErrorsNeverBecomeDirect(t *testing.T) {
	svc, repo := accountEgressTicketFixture(t)
	repo.account.Extra[ProxyModeExtraKey], repo.account.Extra[RandomProxyEmptyPoolPolicyExtraKey] = ProxyModeRandom, RandomProxyEmptyPoolPolicyDirect
	repo.proxyErr = errors.New("pool unavailable")
	_, err := svc.selectOpenAICodexTicketProxy(context.Background(), repo.account)
	require.ErrorIs(t, err, repo.proxyErr)
	require.Empty(t, repo.disabledIDs)
}

func TestCodexTicketAccountModeChangedGroupStopsProbeOrPublication(t *testing.T) {
	for _, stage := range []int{1, 2} {
		svc, repo := accountEgressTicketFixture(t)
		repo.account.Extra[ProxyModeExtraKey] = ProxyModeRandom
		repo.account.Extra[RandomProxyPoolScopeExtraKey], repo.account.Extra[RandomProxyGroupIDExtraKey] = RandomProxyPoolGroup, int64(3)
		repo.groupIDs = []int64{7}
		calls := 0
		svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
			calls++
			if calls == stage {
				repo.groupIDs = []int64{8}
				repo.proxy = &Proxy{ID: 8, Status: StatusActive, Protocol: "http", Host: "other.example", Port: 8080}
			}
			return codexTicketResponse(), nil
		}}
		svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
		require.Equal(t, stage, calls)
		require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
	}
}

func TestCodexTicketAccountModeChangedFixedExitOrModeStopsPublication(t *testing.T) {
	for _, change := range []string{"address", "mode", "disabled", "missing"} {
		for _, stage := range []int{1, 2} {
			t.Run(change+string(rune('0'+stage)), func(t *testing.T) {
				svc, repo := accountEgressTicketFixture(t)
				repo.account.ProxyID, repo.account.Proxy = &repo.proxy.ID, repo.proxy
				calls := 0
				svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
					calls++
					if calls == stage {
						switch change {
						case "address":
							repo.proxy.Host = "changed.example"
						case "mode":
							repo.account.Extra[CodexTicketProxyModeExtraKey] = CodexTicketProxyModeInherit
						case "disabled":
							repo.proxy.Status = StatusDisabled
						case "missing":
							delete(repo.proxies, repo.proxy.ID)
						}
					}
					return codexTicketResponse(), nil
				}}
				svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
				require.Equal(t, stage, calls)
				require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
			})
		}
	}
}
