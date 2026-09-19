package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type balancedAccountProxyStub struct {
	pluginDirectoryProxyRepo
	selections []ProxyPoolSelection
}

func (r *balancedAccountProxyStub) SelectBalancedProxy(_ context.Context, selection ProxyPoolSelection) (*Proxy, error) {
	r.selections = append(r.selections, selection)
	return r.proxy, r.selectionErr
}

func TestBalancedProxySelectionUsesAccountAndPool(t *testing.T) {
	account := randomProxyAccount(RandomProxyEmptyPoolPolicyReject)
	account.Extra[RandomProxyPoolScopeExtraKey] = RandomProxyPoolSelected
	account.Extra[RandomProxyPoolIDsExtraKey] = []int64{9, 7}
	repo := &balancedAccountProxyStub{pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{
		proxy: &Proxy{ID: 7, Status: StatusActive},
	}}
	require.NoError(t, ResolveRandomProxy(context.Background(), account, repo))
	require.Equal(t, []ProxyPoolSelection{{AccountID: 42, IDs: []int64{7, 9}, Restricted: true}}, repo.selections)
	require.Zero(t, repo.globalCalls)
	require.Empty(t, repo.selectedIDs)
	repo.proxy.ID = 99
	require.ErrorIs(t, ResolveRandomProxy(context.Background(), account, repo), ErrRandomProxyUnavailable)
	require.Nil(t, account.ProxyID)
}

func TestBalancedProxySelectionPreservesEmptyPoolPolicies(t *testing.T) {
	for _, policy := range []string{RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDisable, RandomProxyEmptyPoolPolicyDirect} {
		t.Run(policy, func(t *testing.T) {
			repo := &balancedAccountProxyStub{pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{account: pluginDirectoryAccount(policy)}}
			svc := &OpenAIGatewayService{accountRepo: repo}
			identity, err := svc.ResolvePluginOutboundIdentity(context.Background(), 42)
			if policy == RandomProxyEmptyPoolPolicyDirect {
				require.NoError(t, err)
				require.Empty(t, identity.ProxyURL)
			} else {
				require.ErrorIs(t, err, ErrRandomProxyUnavailable)
				require.Nil(t, identity)
			}
			if policy == RandomProxyEmptyPoolPolicyDisable {
				require.Equal(t, []int64{42}, repo.disabledIDs)
			}
			require.Len(t, repo.selections, 1)
			require.Equal(t, int64(42), repo.selections[0].AccountID)
			require.Zero(t, repo.globalCalls)
		})
	}
}

func TestBalancedProxySelectionFailureNeverFallsBackToDirect(t *testing.T) {
	account := randomProxyAccount(RandomProxyEmptyPoolPolicyDirect)
	failure := errors.New("redis unavailable")
	repo := &balancedAccountProxyStub{pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{selectionErr: failure}}
	require.ErrorIs(t, ResolveRandomProxy(context.Background(), account, repo), failure)
	require.Nil(t, account.Proxy)
	require.Nil(t, account.ProxyID)
}

func TestBalancedProxyReuseResolvesCurrentAccountPool(t *testing.T) {
	account := pluginDirectoryAccount(RandomProxyEmptyPoolPolicyReject)
	proxy := &Proxy{ID: 7, Status: StatusActive, Protocol: "http", Host: "proxy.example", Port: 8080}
	repo := &balancedAccountProxyStub{pluginDirectoryProxyRepo: pluginDirectoryProxyRepo{account: account, proxy: proxy}}
	bound := *account
	bound.ProxyID, bound.Proxy = &proxy.ID, proxy
	require.NoError(t, ValidateRandomProxyForReuse(context.Background(), &bound, repo))
	require.Equal(t, []ProxyPoolSelection{{AccountID: 42}}, repo.selections)
	require.Nil(t, account.ProxyID)
}
