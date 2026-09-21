package service

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSharedPoolCatalogModernPriceAndSettlementCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Group, *Account)
		want   bool
	}{
		{name: "funded ordinary group", want: true},
		{name: "funded subscription", change: func(g *Group, _ *Account) { g.SubscriptionType = SubscriptionTypeSubscription }, want: true},
		{name: "funded exclusive", change: func(g *Group, _ *Account) { g.IsExclusive = true }, want: true},
		{name: "funded exclusive subscription", change: func(g *Group, _ *Account) { g.SubscriptionType = SubscriptionTypeSubscription; g.IsExclusive = true }, want: true},
		{name: "underfunded subscription", change: func(g *Group, _ *Account) { g.SubscriptionType = SubscriptionTypeSubscription; g.RateMultiplier = .5 }},
		{name: "invalid group type", change: func(g *Group, _ *Account) { g.SubscriptionType = "unsupported" }},
		{name: "legacy subscription", change: func(g *Group, a *Account) {
			g.SubscriptionType = SubscriptionTypeSubscription
			g.IsSharedPool = true
			delete(a.Extra, SharedPoolDispatchConsentKey)
		}},
		{name: "legacy exclusive", change: func(g *Group, a *Account) {
			g.IsExclusive = true
			g.IsSharedPool = true
			delete(a.Extra, SharedPoolDispatchConsentKey)
		}},
		{name: "equal multiplier", change: func(g *Group, _ *Account) { g.RateMultiplier = 1 }, want: true},
		{name: "underfunded", change: func(g *Group, _ *Account) { g.RateMultiplier = .5 }},
		{name: "free group", change: func(g *Group, _ *Account) { g.RateMultiplier = 0 }},
		{name: "invalid price", change: func(g *Group, _ *Account) { g.RateMultiplier = math.Inf(1) }},
		{name: "missing settlement", change: func(_ *Group, a *Account) { a.SharedPoolSettlement = nil }},
		{name: "invalid settlement", change: func(_ *Group, a *Account) { a.SharedPoolSettlement.Multiplier = math.NaN() }},
		{name: "nonconsent shared", change: func(g *Group, a *Account) { g.IsSharedPool = true; a.Extra[SharedPoolDispatchConsentKey] = false }},
		{name: "underfunded independent image", change: func(g *Group, _ *Account) { g.ImageRateIndependent = true; g.ImageRateMultiplier = .5 }},
		{name: "funded independent image", change: func(g *Group, _ *Account) { g.ImageRateIndependent = true; g.ImageRateMultiplier = 1 }, want: true},
		{name: "invalid independent image", change: func(g *Group, _ *Account) { g.ImageRateIndependent = true; g.ImageRateMultiplier = math.NaN() }},
		{name: "standard ignores subscription-only peak config", change: func(g *Group, _ *Account) {
			g.PeakRateEnabled = true
			g.PeakStart = "00:00"
			g.PeakEnd = "23:59"
			g.PeakRateMultiplier = 0
		}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := Group{Platform: PlatformOpenAI, Status: StatusActive, SubscriptionType: SubscriptionTypeStandard, RateMultiplier: 2}
			a := Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
				Extra:                map[string]any{SharedPoolOwnerKey: 7, SharedPoolEnabledKey: true, SharedPoolDispatchConsentKey: true},
				SharedPoolSettlement: &SharedPoolSettlementTerms{Multiplier: 1, PlatformRateBPS: 500, ProxyRateBPS: 100}}
			if tc.change != nil {
				tc.change(&g, &a)
			}
			require.Equal(t, tc.want, IsSharedPoolAccountAvailable(&g, &a))
		})
	}
}
