package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type regionScheduleRepo struct {
	*codexScheduleRepo
	country  string
	requests []CodexTicketReserveRequest
}

func (r *regionScheduleRepo) ReserveCodexTicket(ctx context.Context, request CodexTicketReserveRequest) (*CodexTicketReservation, error) {
	r.requests = append(r.requests, request)
	return r.codexScheduleRepo.ReserveCodexTicket(ctx, request)
}

func (r *regionScheduleRepo) MatchesProxyRegion(_ context.Context, _ *Proxy, country string) (bool, error) {
	return r.country == country, nil
}

func (r *regionScheduleRepo) GetCodexTicketProxy(context.Context, int64) (*Proxy, error) {
	return r.proxy, nil
}

func (r *regionScheduleRepo) SelectBalancedProxy(context.Context, ProxyPoolSelection) (*Proxy, error) {
	return r.proxy, nil
}

func TestCodexTicketScheduledRegionPassesCountryForEveryProxyMode(t *testing.T) {
	for _, mode := range []string{"random", "inherit", "fixed", "account"} {
		t.Run(mode, func(t *testing.T) {
			svc, base := codexScheduledFixture(t, true)
			r := &regionScheduleRepo{codexScheduleRepo: base, country: "JP"}
			svc.accountRepo = r
			svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = ""
			r.proxy = &Proxy{ID: 7, Status: StatusActive, Protocol: "http", Host: "jp.example", Port: 8080}
			r.account.Extra = map[string]any{CodexTicketProxyModeExtraKey: mode, "proxy_region_mode": "manual", "proxy_region_country": "JP"}
			if mode == "fixed" {
				r.account.Extra[CodexTicketProxyIDExtraKey] = 7
			} else if mode == "account" {
				r.account.ProxyID = &r.proxy.ID
			}
			_, selected, err := svc.reserveCodexTicketHarvest(context.Background(), r.account, "gpt-6-astra", svc.cfg.Gateway.OpenAICodexTicket)
			require.NoError(t, err)
			require.Equal(t, "JP", r.requests[0].Selection.CountryCode)
			require.Equal(t, "JP", selected.policy.countryCode)
		})
	}
}

func TestCodexTicketScheduledRegionRejectsMismatchedReservationAndReleasesIt(t *testing.T) {
	svc, base := codexScheduledFixture(t, true)
	r := &regionScheduleRepo{codexScheduleRepo: base, country: "PH"}
	svc.accountRepo = r
	r.proxy = &Proxy{ID: 7, Status: StatusActive, Protocol: "http", Host: "ph.example", Port: 8080}
	r.account.Extra = map[string]any{CodexTicketProxyModeExtraKey: "random", "proxy_region_mode": "manual", "proxy_region_country": "JP"}
	_, _, err := svc.reserveCodexTicketHarvest(context.Background(), r.account, "gpt-6-astra", svc.cfg.Gateway.OpenAICodexTicket)
	require.Error(t, err)
	require.Len(t, r.finishes, 1)
	require.Equal(t, "canceled", r.finishes[0].Outcome)
}

func TestCodexTicketScheduledRegionPreservesExplicitAccountEmptyDirect(t *testing.T) {
	svc, base := codexScheduledFixture(t, true)
	r := &regionScheduleRepo{codexScheduleRepo: base, country: "JP"}
	svc.accountRepo = r
	r.account.Extra = map[string]any{CodexTicketProxyModeExtraKey: "account", ProxyModeExtraKey: ProxyModeRandom,
		RandomProxyEmptyPoolPolicyExtraKey: RandomProxyEmptyPoolPolicyDirect, "proxy_region_mode": "manual", "proxy_region_country": "JP"}
	_, selected, err := svc.reserveCodexTicketHarvest(context.Background(), r.account, "gpt-6-astra", svc.cfg.Gateway.OpenAICodexTicket)
	require.NoError(t, err)
	require.Empty(t, selected.url)
	require.Equal(t, "JP", selected.policy.countryCode)
	require.Equal(t, "JP", r.requests[0].Selection.CountryCode)
	require.True(t, r.requests[0].AllowDirectOnEmpty, "筛选后空池继续遵循账号明确选择的直连策略")
}
