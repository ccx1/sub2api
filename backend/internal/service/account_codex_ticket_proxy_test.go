package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type accountTicketProxyRepo struct {
	codexTicketControlRepo
	proxy      *Proxy
	proxyErr   error
	selections []ProxyPoolSelection
	failures   int
}

func (r *accountTicketProxyRepo) GetCodexTicketProxy(context.Context, int64) (*Proxy, error) {
	return r.proxy, r.proxyErr
}

func (r *accountTicketProxyRepo) SelectBalancedProxy(_ context.Context, selection ProxyPoolSelection) (*Proxy, error) {
	r.selections = append(r.selections, selection)
	return r.proxy, r.proxyErr
}

func (r *accountTicketProxyRepo) ReportRandomProxyFailure(context.Context, int64, int64) error {
	r.failures++
	return nil
}

func accountTicketProxyFixture(t *testing.T, mode string) (*OpenAIGatewayService, *accountTicketProxyRepo) {
	t.Helper()
	svc, control := codexTicketControlService(t)
	control.account.Extra = map[string]any{CodexTicketProxyModeExtraKey: mode}
	if mode == CodexTicketProxyModeFixed {
		control.account.Extra[CodexTicketProxyIDExtraKey] = int64(7)
	}
	repo := &accountTicketProxyRepo{codexTicketControlRepo: *control,
		proxy: &Proxy{ID: 7, Name: "chosen", Status: StatusActive, Protocol: "http", Host: "chosen.example", Port: 8080}}
	svc.accountRepo = repo
	return svc, repo
}

func TestAccountCodexTicketProxySettingsValidation(t *testing.T) {
	for name, extra := range map[string]map[string]any{
		"default": nil,
		"inherit": {CodexTicketProxyModeExtraKey: "inherit", CodexTicketProxyIDExtraKey: 0},
		"account": {CodexTicketProxyModeExtraKey: "account", CodexTicketProxyIDExtraKey: 0},
		"random":  {CodexTicketProxyModeExtraKey: "random"},
		"fixed":   {CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: json.Number("7")},
	} {
		t.Run(name, func(t *testing.T) { require.NoError(t, ValidateCodexTicketProxyExtra(extra)) })
	}
	for name, extra := range map[string]map[string]any{
		"mode":          {CodexTicketProxyModeExtraKey: "direct"},
		"wrong_type":    {CodexTicketProxyModeExtraKey: true},
		"id_required":   {CodexTicketProxyModeExtraKey: "fixed"},
		"fraction":      {CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: 1.5},
		"string":        {CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: "7"},
		"negative":      {CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: -1},
		"unsafe_number": {CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: int64(1 << 53)},
		"orphan_id":     {CodexTicketProxyIDExtraKey: 7},
		"random_id":     {CodexTicketProxyModeExtraKey: "random", CodexTicketProxyIDExtraKey: 7},
		"account_id":    {CodexTicketProxyModeExtraKey: "account", CodexTicketProxyIDExtraKey: 7},
	} {
		t.Run(name, func(t *testing.T) { require.Error(t, ValidateCodexTicketProxyExtra(extra)) })
	}
}

func TestAccountCodexTicketProxyPartialUpdatesPreserveSettings(t *testing.T) {
	current := map[string]any{CodexTicketProxyModeExtraKey: "fixed", CodexTicketProxyIDExtraKey: 7}
	merged := MergeOpenAICodexTicketExtra(map[string]any{"custom": true}, current)
	require.Equal(t, "fixed", merged[CodexTicketProxyModeExtraKey])
	require.Equal(t, 7, merged[CodexTicketProxyIDExtraKey])
	for _, mode := range []string{"inherit", "account", "random"} {
		merged = MergeOpenAICodexTicketExtra(map[string]any{CodexTicketProxyModeExtraKey: mode}, current)
		require.NoError(t, ValidateCodexTicketProxyExtra(merged))
		require.EqualValues(t, 0, merged[CodexTicketProxyIDExtraKey])
	}
	merged, err := normalizeCodexTicketProxyUpdate(map[string]any{CodexTicketProxyIDExtraKey: 8}, current)
	require.NoError(t, err)
	require.Equal(t, "fixed", merged[CodexTicketProxyModeExtraKey])
	require.Equal(t, 8, merged[CodexTicketProxyIDExtraKey])
	require.Equal(t, 7, current[CodexTicketProxyIDExtraKey])
}

func TestAccountCodexTicketProxyOverridesGlobalAndPreservesBusinessExit(t *testing.T) {
	for _, mode := range []string{"inherit", "random", "fixed"} {
		t.Run(mode, func(t *testing.T) {
			svc, repo := accountTicketProxyFixture(t, mode)
			upstream := &ticketPoolUpstream{}
			svc.httpUpstream = upstream
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			expected := "http://chosen.example:8080"
			if mode == "inherit" {
				expected = "http://harvest.example:8080"
			}
			require.Equal(t, []string{expected, ""}, upstream.proxies)
			require.Nil(t, repo.account.ProxyID)
			require.True(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra").valid(time.Now(), 292))
			if mode == "random" {
				require.Equal(t, []ProxyPoolSelection{{AccountID: repo.account.ID}}, repo.selections)
			} else {
				require.Empty(t, repo.selections)
			}
		})
	}
}

func TestAccountCodexTicketRandomProxyIgnoresBusinessPoolScope(t *testing.T) {
	svc, repo := accountTicketProxyFixture(t, "random")
	repo.account.Extra[ProxyModeExtraKey] = ProxyModeRandom
	repo.account.Extra[RandomProxyPoolScopeExtraKey] = RandomProxyPoolSelected
	repo.account.Extra[RandomProxyPoolIDsExtraKey] = []int64{99}
	repo.account.Extra[RandomProxyMaxReuseMinutesExtraKey] = 30
	_, err := svc.selectOpenAICodexTicketProxy(context.Background(), repo.account)
	require.NoError(t, err)
	require.Equal(t, []ProxyPoolSelection{{AccountID: repo.account.ID}}, repo.selections)
}

func TestAccountCodexTicketFixedProxyUnavailableNeverFallsBack(t *testing.T) {
	for _, reason := range []string{"missing", "disabled", "expired", "error"} {
		t.Run(reason, func(t *testing.T) {
			svc, repo := accountTicketProxyFixture(t, "fixed")
			switch reason {
			case "missing":
				repo.proxy = nil
			case "disabled":
				repo.proxy.Status = StatusDisabled
			case "expired":
				past := time.Now().Add(-time.Hour)
				repo.proxy.ExpiresAt = &past
			case "error":
				repo.proxyErr = errors.New("unavailable")
			}
			upstream := &ticketPoolUpstream{}
			svc.httpUpstream = upstream
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			require.Empty(t, upstream.proxies)
			require.Empty(t, repo.selections)
		})
	}
}

func TestAccountCodexTicketFixedFailureDoesNotReportRandomLease(t *testing.T) {
	svc, repo := accountTicketProxyFixture(t, "fixed")
	upstream := &ticketPoolUpstream{failure: errors.New("connection reset by peer")}
	svc.httpUpstream = upstream
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Equal(t, []string{"http://chosen.example:8080"}, upstream.proxies)
	require.Zero(t, repo.failures)
}

func TestAccountCodexTicketProxyChangesStopInFlightWork(t *testing.T) {
	for _, stage := range []int{1, 2} {
		for _, change := range []string{"mode", "id", "address", "disabled"} {
			t.Run(change+string(rune('0'+stage)), func(t *testing.T) {
				svc, repo := accountTicketProxyFixture(t, "fixed")
				calls := 0
				svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
					calls++
					if calls == stage {
						switch change {
						case "mode":
							repo.account.Extra[CodexTicketProxyModeExtraKey], repo.account.Extra[CodexTicketProxyIDExtraKey] = "inherit", 0
						case "id":
							repo.account.Extra[CodexTicketProxyIDExtraKey] = 8
						case "address":
							repo.proxy.Host = "changed.example"
						case "disabled":
							repo.proxy.Status = StatusDisabled
						}
					}
					return codexTicketResponse(), nil
				}}
				svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
				require.Equal(t, stage, calls)
				require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
			})
		}
	}
}

func TestAccountCodexTicketRandomProxyKeepsValidTicket(t *testing.T) {
	svc, repo := accountTicketProxyFixture(t, "fixed")
	upstream := &ticketPoolUpstream{}
	svc.httpUpstream = upstream
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	previous := svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra")
	require.NotNil(t, previous)
	repo.account.Extra[CodexTicketProxyModeExtraKey], repo.account.Extra[CodexTicketProxyIDExtraKey] = "random", 0
	svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
	require.Len(t, upstream.proxies, 2)
	require.Empty(t, repo.selections)
	require.Equal(t, previous, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
}

func TestAccountCodexTicketInheritedGlobalModeChangeStopsProbeOrPublish(t *testing.T) {
	for _, stage := range []int{1, 2} {
		svc, repo := accountTicketProxyFixture(t, "inherit")
		settings := &codexTicketSettingRepo{codexPolicyMigrationRepoStub: &codexPolicyMigrationRepoStub{values: map[string]string{
			SettingKeyOpenAICodexTicketEnabled: "true", SettingKeyOpenAICodexTicketHarvestProxyMode: "fixed",
			SettingKeyOpenAICodexTicketHarvestProxyURL: "http://harvest.example:8080",
		}}}
		svc.settingService = NewSettingService(settings, svc.cfg)
		calls := 0
		svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
			calls++
			if calls == stage {
				settings.values[SettingKeyOpenAICodexTicketHarvestProxyMode] = "pool"
				svc.settingService.InvalidateProxyPoolSettingsCache()
			}
			return codexTicketResponse(), nil
		}}
		svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
		require.Equal(t, stage, calls)
		require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
	}
}

func TestAccountCodexTicketProxyGlobalURLChangeOnlyAffectsInheritedMode(t *testing.T) {
	for _, mode := range []string{"inherit", "random", "fixed"} {
		t.Run(mode, func(t *testing.T) {
			svc, repo := accountTicketProxyFixture(t, mode)
			calls := 0
			svc.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				calls++
				svc.cfg.Gateway.OpenAICodexTicket.HarvestProxyURL = "http://changed-global.example:8080"
				return codexTicketResponse(), nil
			}}
			svc.probeOnceOpenAICodexTicket(context.Background(), repo.account, "gpt-6-astra")
			if mode == "inherit" {
				require.Equal(t, 1, calls)
				require.Nil(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra"))
			} else {
				require.Equal(t, 2, calls)
				require.True(t, svc.lookupOpenAICodexTicket(repo.account, "gpt-6-astra").valid(time.Now(), 292))
			}
		})
	}
}
