package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type groupAccountProxyStub struct {
	balancedAccountProxyStub
	groupIDs   []int64
	groupCalls []int64
	groupErr   error
}

func (r *groupAccountProxyStub) GetRandomProxyGroupIDs(_ context.Context, groupID int64) ([]int64, error) {
	r.groupCalls = append(r.groupCalls, groupID)
	return r.groupIDs, r.groupErr
}

func randomProxyGroupAccount(policy string) *Account {
	account := pluginDirectoryAccount(policy)
	account.Extra[RandomProxyPoolScopeExtraKey] = RandomProxyPoolGroup
	account.Extra[RandomProxyGroupIDExtraKey] = int64(3)
	return account
}

func TestRandomProxyGroupReflectsMembershipChangesAndRejectsOtherGroups(t *testing.T) {
	account := randomProxyGroupAccount(RandomProxyEmptyPoolPolicyReject)
	repo := &groupAccountProxyStub{groupIDs: []int64{7}}
	repo.proxy = &Proxy{ID: 7, Status: StatusActive}
	require.NoError(t, ResolveRandomProxy(context.Background(), account, repo))
	require.EqualValues(t, 7, *account.ProxyID)
	repo.groupIDs = []int64{8}
	require.ErrorIs(t, ResolveRandomProxy(context.Background(), account, repo), ErrRandomProxyUnavailable)
	require.Nil(t, account.ProxyID)
	repo.proxy = &Proxy{ID: 8, Status: StatusActive}
	require.NoError(t, ResolveRandomProxy(context.Background(), account, repo))
	require.EqualValues(t, 8, *account.ProxyID)
	require.Equal(t, []int64{3, 3, 3}, repo.groupCalls)
	require.Equal(t, []int64{8}, repo.selections[2].IDs)
	require.True(t, repo.selections[2].Restricted)
	require.Zero(t, repo.globalCalls)
}

func TestRandomProxyGroupPluginEmptyPoliciesNeverUseGlobalProxy(t *testing.T) {
	for _, policy := range []string{RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDisable, RandomProxyEmptyPoolPolicyDirect} {
		t.Run(policy, func(t *testing.T) {
			repo := &groupAccountProxyStub{}
			repo.account = randomProxyGroupAccount(policy)
			repo.proxy = &Proxy{ID: 99, Status: StatusActive}
			svc := &OpenAIGatewayService{accountRepo: repo}
			identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), pluginDirectoryScope(), repo.account.ID)
			if policy == RandomProxyEmptyPoolPolicyDirect {
				require.NoError(t, err)
				require.Empty(t, identity.ProxyURL)
			} else {
				require.ErrorIs(t, err, ErrRandomProxyUnavailable)
				require.Nil(t, identity)
			}
			require.Equal(t, policy == RandomProxyEmptyPoolPolicyDisable, len(repo.disabledIDs) > 0)
			require.Len(t, repo.selections, 1)
			require.True(t, repo.selections[0].Restricted)
			require.Empty(t, repo.selections[0].IDs)
			require.Zero(t, repo.globalCalls)
		})
	}
}

func TestRandomProxyGroupResolutionErrorNeverBecomesDirect(t *testing.T) {
	account := randomProxyGroupAccount(RandomProxyEmptyPoolPolicyDirect)
	failure := errors.New("group database unavailable")
	repo := &groupAccountProxyStub{groupErr: failure}
	require.ErrorIs(t, ResolveRandomProxy(context.Background(), account, repo), failure)
	require.Empty(t, repo.selections)
	require.Nil(t, account.ProxyID)
	require.Zero(t, repo.globalCalls)
	repo.groupErr, repo.groupIDs = nil, []int64{7}
	repo.proxy, repo.selectionErr = &Proxy{ID: 99, Status: StatusActive}, failure
	require.ErrorIs(t, ResolveRandomProxy(context.Background(), account, repo), failure)
	require.Nil(t, account.ProxyID)
}

func TestRandomProxyGroupMissingResolverAndInvalidIDStayRestricted(t *testing.T) {
	account := randomProxyGroupAccount(RandomProxyEmptyPoolPolicyReject)
	repo := &balancedAccountProxyStub{pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{proxy: &Proxy{ID: 99, Status: StatusActive}}}
	require.ErrorIs(t, ResolveRandomProxy(context.Background(), account, repo), ErrRandomProxyUnavailable)
	require.True(t, repo.selections[0].Restricted)
	groupRepo := &groupAccountProxyStub{groupIDs: []int64{7}}
	account.Extra[RandomProxyGroupIDExtraKey] = nil
	require.ErrorIs(t, ResolveRandomProxy(context.Background(), account, groupRepo), ErrRandomProxyUnavailable)
	require.Empty(t, groupRepo.groupCalls)
	require.True(t, groupRepo.selections[0].Restricted)
}

func TestRandomProxyGroupConnectionReuseChecksCurrentMembership(t *testing.T) {
	account := randomProxyGroupAccount(RandomProxyEmptyPoolPolicyReject)
	proxy := &Proxy{ID: 7, Protocol: "http", Host: "proxy.example", Port: 8080, Status: StatusActive}
	bound := *account
	bound.ProxyID, bound.Proxy = &proxy.ID, proxy
	repo := &groupAccountProxyStub{groupIDs: []int64{7}}
	repo.account, repo.proxy = account, proxy
	require.NoError(t, ValidateRandomProxyForReuse(context.Background(), &bound, repo))
	repo.groupIDs = []int64{8}
	repo.proxy = &Proxy{ID: 8, Protocol: "http", Host: "other.example", Port: 8080, Status: StatusActive}
	require.ErrorIs(t, ValidateRandomProxyForReuse(context.Background(), &bound, repo), ErrRandomProxyChanged)
}

func TestValidateRandomProxyGroupExtra(t *testing.T) {
	for _, raw := range []string{
		`{"random_proxy_pool_scope":"group"}`,
		`{"random_proxy_pool_scope":"group","random_proxy_group_id":null}`,
		`{"random_proxy_pool_scope":"group","random_proxy_group_id":0}`,
		`{"random_proxy_pool_scope":"group","random_proxy_group_id":-1}`,
		`{"random_proxy_pool_scope":"group","random_proxy_group_id":1.5}`,
		`{"random_proxy_pool_scope":"group","random_proxy_group_id":"3"}`,
		`{"random_proxy_pool_scope":"group","random_proxy_group_id":9007199254740992}`,
	} {
		var extra map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &extra))
		require.Error(t, ValidateRandomProxyPoolExtra(extra), raw)
	}
	account := randomProxyGroupAccount(RandomProxyEmptyPoolPolicyReject)
	require.NoError(t, ValidateRandomProxyPoolExtra(account.Extra))
	account.Extra[RandomProxyPoolScopeExtraKey] = RandomProxyPoolAll
	account.Extra[RandomProxyGroupIDExtraKey] = nil
	require.NoError(t, ValidateRandomProxyPoolExtra(account.Extra))
}

func TestCodexTicketInheritsDynamicProxyGroupAndBinding(t *testing.T) {
	account := randomProxyGroupAccount(RandomProxyEmptyPoolPolicyReject)
	repo := &groupAccountProxyStub{groupIDs: []int64{7}}
	repo.proxy = &Proxy{ID: 7, Status: StatusActive, Protocol: "http", Host: "group.example", Port: 8080}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true}, &ticketPoolUpstream{})
	svc.accountRepo = repo
	proxy, err := svc.selectOpenAICodexTicketProxy(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "http://group.example:8080", proxy.url)
	require.True(t, repo.selections[0].Restricted)
	require.Equal(t, []int64{7}, repo.selections[0].IDs)
	before := svc.codexTicketBinding(account)
	account.Extra[RandomProxyGroupIDExtraKey] = int64(4)
	require.NotEqual(t, before, svc.codexTicketBinding(account))
	repo.groupIDs = nil
	_, err = svc.selectOpenAICodexTicketProxy(context.Background(), account)
	require.ErrorIs(t, err, ErrRandomProxyUnavailable)
	require.Zero(t, repo.globalCalls)
}
