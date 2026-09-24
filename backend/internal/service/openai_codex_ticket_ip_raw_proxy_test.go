package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCodexTicketIPProtectionRejectsRawHarvestProxyBeforeScheduling(t *testing.T) {
	for _, rawURL := range []string{
		"http://private-user:private-password@203.0.113.10:8080",
		"socks5h://private-user:private-password@[2001:db8::10]:1080",
		"https://private-user:private-password@proxy.invalid:8443",
	} {
		for _, scheduler := range []bool{false, true} {
			name := map[bool]string{false: "without scheduler", true: "with scheduler"}[scheduler]
			t.Run(name, func(t *testing.T) {
				svc, base := codexScheduledFixture(t, false)
				repo := &regionScheduleRepo{codexScheduleRepo: base}
				svc.accountRepo = repo
				if !scheduler {
					svc.accountRepo = base.codexTicketControlRepo
				}
				svc.cfg.Gateway.OpenAICodexTicket.Protection.ProxyIPProtectionEnabled = true
				svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = rawURL
				calls := 0
				svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
					calls++
					return codexTicketResponse(), nil
				}}
				_, _, err := svc.reserveCodexTicketHarvest(context.Background(), repo.account, "gpt-6-astra", svc.cfg.Gateway.OpenAICodexTicket)
				require.Error(t, err)
				if scheduler {
					require.ErrorContains(t, err, "requires a managed harvest proxy")
					require.ErrorContains(t, err, "proxy management")
				} else {
					require.ErrorContains(t, err, "shared scheduler unavailable")
				}
				require.NotContains(t, err.Error(), "private-")
				require.NotContains(t, err.Error(), rawURL)
				svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
				require.Zero(t, calls)
				require.Empty(t, repo.requests)
				require.Zero(t, repo.started)
			})
		}
	}
}

func TestCodexTicketIPProtectionDisabledPreservesRawProxyURL(t *testing.T) {
	svc, base := codexScheduledFixture(t, false)
	repo := &regionScheduleRepo{codexScheduleRepo: base}
	svc.accountRepo = repo
	rawURL := "socks5h://user-only@203.0.113.10:1080"
	svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = rawURL
	_, selected, err := svc.reserveCodexTicketHarvest(context.Background(), repo.account, "gpt-6-astra", svc.cfg.Gateway.OpenAICodexTicket)
	require.NoError(t, err)
	require.Equal(t, rawURL, selected.url)
	require.Len(t, repo.requests, 1)
	require.False(t, repo.requests[0].PoolMode)
	require.Nil(t, repo.requests[0].FixedProxy)
}

func TestCodexTicketIPProtectionAllowsManagedProxyAndExplicitDirect(t *testing.T) {
	for _, mode := range []string{CodexTicketProxyModeFixed, CodexTicketProxyModeAccount} {
		t.Run(mode, func(t *testing.T) {
			svc, base := codexScheduledFixture(t, false)
			repo := &regionScheduleRepo{codexScheduleRepo: base}
			svc.accountRepo = repo
			svc.cfg.Gateway.OpenAICodexTicket.Protection.ProxyIPProtectionEnabled = true
			repo.account.Extra[CodexTicketProxyModeExtraKey] = mode
			if mode == CodexTicketProxyModeFixed {
				repo.proxy = &Proxy{ID: 7, Status: StatusActive, Protocol: "http", Host: "203.0.113.10", Port: 8080}
				repo.account.Extra[CodexTicketProxyIDExtraKey] = int64(7)
			}
			_, selected, err := svc.reserveCodexTicketHarvest(context.Background(), repo.account, "gpt-6-astra", svc.cfg.Gateway.OpenAICodexTicket)
			require.NoError(t, err)
			require.Len(t, repo.requests, 1)
			if mode == CodexTicketProxyModeFixed {
				require.Equal(t, repo.proxy, repo.requests[0].FixedProxy)
				require.Equal(t, repo.proxy.URL(), selected.url)
			} else {
				require.Empty(t, selected.url)
				require.Nil(t, repo.requests[0].FixedProxy)
			}
		})
	}
}
