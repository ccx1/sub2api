package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type randomProxySelectorStub struct {
	proxy *Proxy
	err   error
	calls int
}

func (s *randomProxySelectorStub) SelectRandomActiveProxy(context.Context) (*Proxy, error) {
	s.calls++
	return s.proxy, s.err
}

func randomProxyAccount(policy string) *Account {
	extra := map[string]any{ProxyModeExtraKey: ProxyModeRandom}
	if policy != "" {
		extra[RandomProxyEmptyPoolPolicyExtraKey] = policy
	}
	return &Account{ID: 42, Extra: extra}
}

func TestResolveRandomProxyAssignsEphemeralProxy(t *testing.T) {
	proxy := &Proxy{ID: 7, Status: StatusActive}
	selector := &randomProxySelectorStub{proxy: proxy}
	account := randomProxyAccount("")

	if err := ResolveRandomProxy(context.Background(), account, selector); err != nil {
		t.Fatalf("ResolveRandomProxy() error = %v", err)
	}
	if selector.calls != 1 {
		t.Fatalf("selector calls = %d, want 1", selector.calls)
	}
	if account.Proxy != proxy || account.ProxyID == nil || *account.ProxyID != proxy.ID {
		t.Fatalf("account proxy was not assigned: proxy=%#v proxy_id=%v", account.Proxy, account.ProxyID)
	}
}

func TestResolveAccountProxyPoolSelectionCarriesRegionFallback(t *testing.T) {
	account := randomProxyAccount(RandomProxyEmptyPoolPolicyReject)
	selection, err := ResolveAccountProxyPoolSelection(context.Background(), account, nil)
	require.NoError(t, err)
	require.True(t, selection.AllowCountryFallback)

	account.Extra[RandomProxyRegionFallbackExtraKey] = RandomProxyRegionFallbackPool
	selection, err = ResolveAccountProxyPoolSelection(context.Background(), account, nil)
	require.NoError(t, err)
	require.True(t, selection.AllowCountryFallback)

	account.Extra[RandomProxyRegionFallbackExtraKey] = RandomProxyRegionFallbackNone
	selection, err = ResolveAccountProxyPoolSelection(context.Background(), account, nil)
	require.NoError(t, err)
	require.False(t, selection.AllowCountryFallback)
}

func TestResolveRandomProxyEmptyPoolPolicies(t *testing.T) {
	cases := []struct {
		name       string
		policy     string
		wantErr    bool
		wantPolicy string
	}{
		{name: "default rejects", wantErr: true, wantPolicy: RandomProxyEmptyPoolPolicyReject},
		{name: "explicit reject", policy: RandomProxyEmptyPoolPolicyReject, wantErr: true, wantPolicy: RandomProxyEmptyPoolPolicyReject},
		{name: "disable reports policy", policy: RandomProxyEmptyPoolPolicyDisable, wantErr: true, wantPolicy: RandomProxyEmptyPoolPolicyDisable},
		{name: "direct allows request", policy: RandomProxyEmptyPoolPolicyDirect, wantErr: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := randomProxyAccount(tc.policy)
			staleID := int64(99)
			account.ProxyID = &staleID
			account.Proxy = &Proxy{ID: staleID, Status: StatusActive}

			err := ResolveRandomProxy(context.Background(), account, &randomProxySelectorStub{})
			if tc.wantErr {
				if !errors.Is(err, ErrRandomProxyUnavailable) {
					t.Fatalf("error = %v, want ErrRandomProxyUnavailable", err)
				}
				if got := RandomProxyUnavailablePolicy(err); got != tc.wantPolicy {
					t.Fatalf("policy = %q, want %q", got, tc.wantPolicy)
				}
			} else if err != nil {
				t.Fatalf("ResolveRandomProxy() error = %v", err)
			}
			if account.ProxyID != nil || account.Proxy != nil {
				t.Fatalf("empty-pool path left a stale proxy association")
			}
		})
	}
}

func TestResolveRandomProxyRejectsInactiveOrExpiredProxy(t *testing.T) {
	for _, proxy := range []*Proxy{
		{ID: 1, Status: StatusDisabled},
		{ID: 2, Status: StatusActive, ExpiresAt: randomProxyTimePtr(time.Now().Add(-time.Minute))},
	} {
		account := randomProxyAccount("")
		err := ResolveRandomProxy(context.Background(), account, &randomProxySelectorStub{proxy: proxy})
		if !errors.Is(err, ErrRandomProxyUnavailable) {
			t.Fatalf("error = %v, want ErrRandomProxyUnavailable", err)
		}
	}
}

func TestNormalizeProxyModeExtra(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
		want  map[string]any
	}{
		{name: "nil map stays nil", input: nil, want: nil},
		{name: "random kept", input: map[string]any{ProxyModeExtraKey: " Random "}, want: map[string]any{ProxyModeExtraKey: ProxyModeRandom}},
		{name: "invalid mode dropped", input: map[string]any{ProxyModeExtraKey: "fixed"}, want: map[string]any{}},
		{name: "disabled random clears group", input: map[string]any{ProxyModeExtraKey: nil, RandomProxyGroupIDExtraKey: int64(3)}, want: map[string]any{}},
		{name: "fixed clears group", input: map[string]any{ProxyModeExtraKey: "fixed", RandomProxyGroupIDExtraKey: int64(3)}, want: map[string]any{}},
		{name: "random keeps group", input: map[string]any{ProxyModeExtraKey: "random", RandomProxyGroupIDExtraKey: float64(3)}, want: map[string]any{ProxyModeExtraKey: "random", RandomProxyGroupIDExtraKey: int64(3)}},
		{name: "policy canonicalized", input: map[string]any{RandomProxyEmptyPoolPolicyExtraKey: " Direct "}, want: map[string]any{RandomProxyEmptyPoolPolicyExtraKey: RandomProxyEmptyPoolPolicyDirect}},
		{name: "invalid policy dropped", input: map[string]any{RandomProxyEmptyPoolPolicyExtraKey: "drop"}, want: map[string]any{}},
		{name: "region fallback canonicalized", input: map[string]any{ProxyModeExtraKey: "random", RandomProxyRegionFallbackExtraKey: " Pool "}, want: map[string]any{ProxyModeExtraKey: ProxyModeRandom, RandomProxyRegionFallbackExtraKey: RandomProxyRegionFallbackPool}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeProxyModeExtra(tc.input)
			if len(got) != len(tc.want) {
				t.Fatalf("NormalizeProxyModeExtra() = %v, want %v", got, tc.want)
			}
			for key, wantValue := range tc.want {
				if got[key] != wantValue {
					t.Fatalf("NormalizeProxyModeExtra()[%q] = %v, want %v", key, got[key], wantValue)
				}
			}
		})
	}
}

func randomProxyTimePtr(value time.Time) *time.Time { return &value }
