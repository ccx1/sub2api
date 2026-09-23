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

type ticketPoolUpstream struct {
	HTTPUpstream
	proxies []string
	failure error
	status  int
}

func (u *ticketPoolUpstream) Do(_ *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	u.proxies = append(u.proxies, proxy)
	if u.failure != nil {
		return nil, u.failure
	}
	response := codexTicketResponse()
	if u.status != 0 {
		response.StatusCode = u.status
	}
	return response, nil
}

func (r *balancedAccountProxyStub) UpdateExtra(context.Context, int64, map[string]any) error {
	return nil
}

func TestCodexTicketPoolActuallyUsesSelectedEgress(t *testing.T) {
	account := ticketTestAccount(42)
	upstream := &ticketPoolUpstream{}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, upstream)
	repo := &balancedAccountProxyStub{pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{
		proxy: &Proxy{ID: 7, Status: StatusActive, Protocol: "socks5", Host: "pool.example", Port: 1080},
	}}
	svc.accountRepo = repo
	svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
	require.Equal(t, []string{"socks5://pool.example:1080", ""}, upstream.proxies)
	require.Equal(t, []ProxyPoolSelection{{AccountID: 42}}, repo.selections)
	require.True(t, svc.lookupOpenAICodexTicket(account, "gpt-6-astra").valid(time.Now(), 292))
	require.Nil(t, account.ProxyID, "打票出口不得覆盖账号业务出口")
}

func TestCodexTicketPoolNeverUsesFixedFallbackOrDirect(t *testing.T) {
	for _, failure := range []error{nil, errors.New("redis unavailable")} {
		upstream := &ticketPoolUpstream{}
		svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://fixed.example:8080"}, upstream)
		svc.settingService = NewSettingService(&codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
			"openai_codex_ticket_harvest_proxy_mode": "pool",
		}}}, svc.cfg)
		svc.accountRepo = &balancedAccountProxyStub{pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{selectionErr: failure}}
		svc.probeOnceOpenAICodexTicket(context.Background(), ticketTestAccount(42), "gpt-6-astra")
		require.Empty(t, upstream.proxies)
	}
}

func TestCodexTicketFixedModeKeepsConfiguredProxy(t *testing.T) {
	upstream := &ticketPoolUpstream{}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://fixed.example:8080"}, upstream)
	repo := &balancedAccountProxyStub{}
	svc.accountRepo = repo
	svc.probeOnceOpenAICodexTicket(context.Background(), ticketTestAccount(42), "gpt-6-astra")
	require.Equal(t, []string{"http://fixed.example:8080", ""}, upstream.proxies)
	require.Empty(t, repo.selections)
}

func TestCodexTicketPoolHonorsAccountAffinityConfiguration(t *testing.T) {
	account := ticketTestAccount(42)
	account.Extra = map[string]any{}
	account.Extra[ProxyModeExtraKey] = ProxyModeRandom
	account.Extra[RandomProxyPoolScopeExtraKey] = RandomProxyPoolSelected
	account.Extra[RandomProxyPoolIDsExtraKey] = []int64{7, 9}
	account.Extra[RandomProxyMaxReuseMinutesExtraKey] = 30
	repo := &balancedAccountProxyStub{pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{
		proxy: &Proxy{ID: 7, Status: StatusActive, Protocol: "http", Host: "pool.example", Port: 8080},
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, &ticketPoolUpstream{})
	svc.accountRepo = repo
	_, err := svc.selectOpenAICodexTicketProxy(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, []ProxyPoolSelection{{AccountID: 42, IDs: []int64{7, 9}, Restricted: true, MaxReuseDuration: 30 * time.Minute}}, repo.selections)
}

func TestCodexTicketGlobalRandomAndInheritKeepDifferentPoolScopes(t *testing.T) {
	for _, tc := range []struct {
		mode          string
		wantMode      string
		wantInherited bool
	}{
		{mode: OpenAICodexTicketHarvestProxyModeRandom, wantMode: CodexTicketProxyModeRandom},
		{mode: OpenAICodexTicketHarvestProxyModeInherit, wantMode: CodexTicketProxyModeRandom, wantInherited: true},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			account := ticketTestAccount(42)
			settings := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
				SettingKeyOpenAICodexTicketHarvestProxyMode: tc.mode,
			}}}
			svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, nil)
			svc.accountRepo = &balancedAccountProxyStub{}
			svc.settingService = NewSettingService(settings, svc.cfg)

			policy, err := svc.codexTicketProxyPolicy(context.Background(), account)
			require.NoError(t, err)
			require.Equal(t, tc.wantMode, policy.mode)
			require.Equal(t, tc.wantInherited, policy.inherited)
		})
	}
}
