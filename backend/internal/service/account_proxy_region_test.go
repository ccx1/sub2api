package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAccountProxyRegionCountry(t *testing.T) {
	for _, tc := range []struct {
		name, mode, country, currency, priceCountry, want string
		wantErr                                           bool
	}{
		{name: "existing account remains unrestricted", currency: "JPY"},
		{name: "yen", mode: "billing", currency: "JPY", want: "JP"},
		{name: "peso", mode: "billing", currency: "PHP", want: "PH"},
		{name: "explicit pricing country wins", mode: "billing", currency: "JPY", priceCountry: " ph ", want: "PH"},
		{name: "dollar ambiguous", mode: "billing", currency: "USD", wantErr: true},
		{name: "euro ambiguous", mode: "billing", currency: "EUR", wantErr: true},
		{name: "missing evidence", mode: "billing", wantErr: true},
		{name: "manual overrides currency", mode: "manual", country: " us ", currency: "JPY", want: "US"},
		{name: "manual missing", mode: "manual", wantErr: true},
		{name: "invalid country", mode: "manual", country: "Japan", wantErr: true},
		{name: "invalid mode fails closed", mode: "anything", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{Extra: map[string]any{ProxyRegionModeExtraKey: tc.mode, ProxyRegionCountryExtraKey: tc.country},
				Credentials: map[string]any{"billing_currency": tc.currency, "price_country": tc.priceCountry}}
			got, err := account.ProxyRegionCountry()
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestOAuthProxyRegionUsesSubscriptionCountryUnlessDisabled(t *testing.T) {
	account := &Account{Type: AccountTypeOAuth, Credentials: map[string]any{"price_country": "JP"}, Extra: map[string]any{}}
	country, err := account.ProxyRegionCountry()
	require.NoError(t, err)
	require.Equal(t, "JP", country)

	account.Extra[ProxyRegionModeExtraKey] = "off"
	country, err = account.ProxyRegionCountry()
	require.NoError(t, err)
	require.Empty(t, country)
}

func TestPreserveAccountProxyRegion_DoesNotOverwriteConcurrentSettings(t *testing.T) {
	current := map[string]any{ProxyRegionModeExtraKey: "manual", ProxyRegionCountryExtraKey: "PH"}
	stale := map[string]any{ProxyRegionModeExtraKey: "billing", ProxyRegionCountryExtraKey: "JP", "other": true}
	got := PreserveAccountProxyRegion(context.Background(), current, stale)
	require.Equal(t, "manual", got[ProxyRegionModeExtraKey])
	require.Equal(t, "PH", got[ProxyRegionCountryExtraKey])
	require.Equal(t, true, got["other"])
	ctx := WithAccountProxyRegionWrite(context.Background(), map[string]any{ProxyRegionModeExtraKey: "off"})
	got = PreserveAccountProxyRegion(ctx, current, map[string]any{ProxyRegionModeExtraKey: "off"})
	require.Equal(t, "off", got[ProxyRegionModeExtraKey])
	require.Equal(t, "PH", got[ProxyRegionCountryExtraKey])
}

func TestProxyRegionSurvivesRandomProxyModeChanges(t *testing.T) {
	extra := map[string]any{ProxyModeExtraKey: nil, ProxyRegionModeExtraKey: "manual", ProxyRegionCountryExtraKey: "jp"}
	got := NormalizeProxyModeExtra(extra)
	require.Equal(t, "manual", got[ProxyRegionModeExtraKey])
	require.Equal(t, "JP", got[ProxyRegionCountryExtraKey])
	merged := mergeRandomProxyRoutingExtra(map[string]any{"other": true}, got)
	require.Equal(t, "JP", merged[ProxyRegionCountryExtraKey])
}
