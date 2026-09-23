package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type regionTicketRepo struct {
	accountTicketProxyRepo
	country string
}

func (r *regionTicketRepo) MatchesProxyRegion(_ context.Context, _ *Proxy, country string) (bool, error) {
	return r.country == country, nil
}

func TestCodexTicketRegionAppliesToIndependentAndInheritedPools(t *testing.T) {
	for _, mode := range []string{CodexTicketProxyModeRandom, CodexTicketProxyModeInherit} {
		t.Run(mode, func(t *testing.T) {
			svc, original := accountTicketProxyFixture(t, mode)
			r := &regionTicketRepo{accountTicketProxyRepo: *original, country: "JP"}
			svc.accountRepo = r
			svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = ""
			r.account.Extra["proxy_region_mode"], r.account.Extra["proxy_region_country"] = "manual", "JP"
			selected, err := svc.selectOpenAICodexTicketProxy(context.Background(), r.account)
			require.NoError(t, err)
			require.Equal(t, "JP", r.selections[0].CountryCode)
			require.Equal(t, "JP", selected.policy.countryCode)
			r.country = "PH"
			_, err = svc.selectOpenAICodexTicketProxy(context.Background(), r.account)
			require.Error(t, err)
		})
	}
}

func TestCodexTicketRegionFixedAndAccountNeverUseOtherCountry(t *testing.T) {
	for _, mode := range []string{CodexTicketProxyModeFixed, CodexTicketProxyModeAccount} {
		t.Run(mode, func(t *testing.T) {
			svc, original := accountTicketProxyFixture(t, mode)
			r := &regionTicketRepo{accountTicketProxyRepo: *original, country: "JP"}
			svc.accountRepo = r
			r.account.Extra["proxy_region_mode"], r.account.Extra["proxy_region_country"] = "manual", "JP"
			if mode == CodexTicketProxyModeAccount {
				r.account.ProxyID = &r.proxy.ID
			}
			selected, err := svc.selectOpenAICodexTicketProxy(context.Background(), r.account)
			require.NoError(t, err)
			require.Equal(t, "JP", selected.policy.countryCode)
			r.country = "PH"
			_, err = svc.selectOpenAICodexTicketProxy(context.Background(), r.account)
			require.Error(t, err)
		})
	}
}

func TestCodexTicketRegionRejectsUnverifiedGlobalProxy(t *testing.T) {
	svc, repo := accountTicketProxyFixture(t, CodexTicketProxyModeInherit)
	repo.account.Extra["proxy_region_mode"], repo.account.Extra["proxy_region_country"] = "manual", "JP"
	_, err := svc.selectOpenAICodexTicketProxy(context.Background(), repo.account)
	require.ErrorContains(t, err, "global harvest proxy")
}

func TestCodexTicketRegionChangeStopsInFlightPublishing(t *testing.T) {
	for _, stage := range []int{1, 2} {
		svc, original := accountTicketProxyFixture(t, CodexTicketProxyModeFixed)
		r := &regionTicketRepo{accountTicketProxyRepo: *original, country: "JP"}
		svc.accountRepo = r
		r.account.Extra["proxy_region_mode"], r.account.Extra["proxy_region_country"] = "manual", "JP"
		r.account.ProxyID, r.account.Proxy = &r.proxy.ID, r.proxy
		calls := 0
		svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
			calls++
			if calls == stage {
				r.account.Extra["proxy_region_country"], r.country = "PH", "PH"
			}
			return codexTicketResponse(), nil
		}}
		svc.probeOnceOpenAICodexTicket(context.Background(), r.account, "gpt-6-astra")
		require.Equal(t, stage, calls)
		require.Nil(t, svc.lookupOpenAICodexTicket(r.account, "gpt-6-astra"))
	}
}
