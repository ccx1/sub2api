package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type regionRoutingRepo struct {
	balancedAccountProxyStub
	country string
	err     error
}

func (r *regionRoutingRepo) MatchesProxyRegion(_ context.Context, _ *Proxy, country string) (bool, error) {
	return country == r.country, r.err
}

func (r *regionRoutingRepo) GetCodexTicketProxy(context.Context, int64) (*Proxy, error) {
	return r.proxy, r.selectionErr
}

func regionRoutingAccount() *Account {
	a := randomProxyAccount(RandomProxyEmptyPoolPolicyReject)
	a.Extra["proxy_region_mode"], a.Extra["proxy_region_country"] = "manual", "JP"
	return a
}

func TestProxyRegionRandomSelectionUsesCountryAndPreservesExplicitDirect(t *testing.T) {
	for _, policy := range []string{RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDirect} {
		a := regionRoutingAccount()
		a.Extra[RandomProxyEmptyPoolPolicyExtraKey] = policy
		r := &regionRoutingRepo{country: "JP"}
		err := ResolveRandomProxyFromSource(context.Background(), a, r)
		if policy == RandomProxyEmptyPoolPolicyDirect {
			require.NoError(t, err)
		} else {
			require.ErrorIs(t, err, ErrRandomProxyUnavailable)
		}
		require.Equal(t, "JP", r.selections[0].CountryCode)
		require.Nil(t, a.ProxyID)
	}
}

func TestProxyRegionLegacySelectorNeverFallsBackToUnrestrictedPool(t *testing.T) {
	a := regionRoutingAccount()
	a.Extra[RandomProxyEmptyPoolPolicyExtraKey] = RandomProxyEmptyPoolPolicyDirect
	r := &pluginDirectoryProxyRepo{proxy: &Proxy{ID: 1, Status: StatusActive}}
	require.Error(t, ResolveRandomProxyFromSource(context.Background(), a, r))
	require.Zero(t, r.globalCalls)
	require.Nil(t, a.Proxy)
}

func TestProxyRegionFixedProxyLoadsAndChecksCountry(t *testing.T) {
	a := regionRoutingAccount()
	a.Extra[ProxyModeExtraKey] = "fixed"
	id := int64(7)
	a.ProxyID = &id
	r := &regionRoutingRepo{country: "JP"}
	r.proxy = &Proxy{ID: id, Status: StatusActive, Protocol: "http", Host: "jp.example", Port: 8080}
	require.NoError(t, ResolveRandomProxyFromSource(context.Background(), a, r))
	require.Equal(t, r.proxy, a.Proxy)
	r.country = "PH"
	require.Error(t, ResolveRandomProxyFromSource(context.Background(), a, r))
	r.country, r.err = "JP", errors.New("redis unavailable")
	require.ErrorContains(t, ResolveRandomProxyFromSource(context.Background(), a, r), "redis unavailable")
	a.ProxyID, a.Proxy = nil, nil
	require.Error(t, ResolveRandomProxyFromSource(context.Background(), a, r))
}

func TestProxyRegionFixedWebSocketReuseChecksCurrentPolicy(t *testing.T) {
	a := regionRoutingAccount()
	a.Extra[ProxyModeExtraKey] = "fixed"
	a.Status, a.Schedulable = StatusActive, true
	id := int64(7)
	a.ProxyID, a.Proxy = &id, &Proxy{ID: id, Status: StatusActive, Host: "jp.example", Protocol: "http", Port: 8080}
	r := &regionRoutingRepo{country: "JP"}
	r.account, r.proxy = a, a.Proxy
	bound := *a
	require.NoError(t, ValidateRandomProxyForReuse(context.Background(), &bound, r))
	a.Extra["proxy_region_country"] = "PH"
	require.Error(t, ValidateRandomProxyForReuse(context.Background(), &bound, r))
	a.Extra["proxy_region_country"], r.country = "JP", "JP"
	replacement := *a.Proxy
	replacement.Host = "replacement.example"
	a.Proxy, r.proxy = &replacement, &replacement
	require.ErrorIs(t, ValidateRandomProxyForReuse(context.Background(), &bound, r), ErrRandomProxyChanged)
}

func TestProxyRegionFixedProxyReloadsAddressFromRepository(t *testing.T) {
	a := regionRoutingAccount()
	a.Extra[ProxyModeExtraKey] = "fixed"
	id := int64(7)
	a.ProxyID, a.Proxy = &id, &Proxy{ID: id, Status: StatusActive, Protocol: "http", Host: "stale.example", Port: 8080}
	r := &regionRoutingRepo{country: "JP"}
	r.proxy = &Proxy{ID: id, Status: StatusActive, Protocol: "http", Host: "current.example", Port: 8080}
	require.NoError(t, ResolveRandomProxyFromSource(context.Background(), a, r))
	require.Equal(t, "current.example", a.Proxy.Host)
}
