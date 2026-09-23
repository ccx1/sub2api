package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketCredentialModesReachBusiness(t *testing.T) {
	for _, mode := range []string{"state", "cookie_state", "cookie"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			state := fakeCodexTicketState(292)
			if mode == "cookie" {
				state = ""
			}
			upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				calls++
				if calls == 2 {
					require.Equal(t, "session=credential-private", req.Header.Get("Cookie"))
					require.Equal(t, state, req.Header.Get(openAICodexTurnStateHeader))
				}
				response := codexTicketCompletedResponse("gpt-6-astra", state)
				response.Header.Add("Set-Cookie", "session=credential-private; Path=/backend-api; Secure; HttpOnly; Max-Age=60")
				return response, nil
			}}
			cfg := config.OpenAICodexTicketConfig{Enabled: true, CredentialMode: mode, HarvestProxyURL: "http://harvest.example:8080", FailClosed: true}
			svc, account := ticketTestService(t, cfg, upstream), ticketTestAccount(41)
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			require.Equal(t, 2, calls)
			ticket := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
			require.NotNil(t, ticket)
			req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
			require.NoError(t, err)
			req.Header.Set(openAICodexTurnStateHeader, "client-state")
			require.NoError(t, svc.applyOpenAICodexTicketRequest(account, "gpt-6-astra", req))
			require.Equal(t, state, req.Header.Get(openAICodexTurnStateHeader))
			if mode == "state" {
				require.Empty(t, req.Header.Get("Cookie"))
				return
			}
			require.Equal(t, "session=credential-private", req.Header.Get("Cookie"))
			require.LessOrEqual(t, time.Until(ticket.ExpiresAt), 60*time.Second)
			encoded, err := json.Marshal(ticket)
			require.NoError(t, err)
			var stored map[string]any
			require.NoError(t, json.Unmarshal(encoded, &stored))
			account.Extra = map[string]any{openAICodexTicketExtraKey("gpt-6-astra"): stored}
			restarted := ticketTestService(t, cfg, nil)
			headers := make(http.Header)
			require.NoError(t, restarted.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", headers))
			require.Equal(t, "session=credential-private", headers.Get("Cookie"))
			require.Empty(t, RedactOpenAICodexTicketExtra(account.Extra))
			headers = make(http.Header)
			require.ErrorIs(t, restarted.applyOpenAICodexTicket(context.Background(), ticketTestAccount(42), "gpt-6-astra", headers), ErrOpenAICodexTicketUnavailable)
			require.Empty(t, headers.Get("Cookie"))
			require.True(t, restarted.openAICodexTicketBlocksAccount(ticketTestAccount(42), "gpt-6-astra"))
		})
	}
}

func TestCodexTicketCookieExpiryAndModeChanges(t *testing.T) {
	account := ticketTestAccount(41)
	cfg := config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{Enabled: true, CredentialMode: "cookie"})
	ticket := &openAICodexTicket{AccountID: account.ID, Model: "gpt-6-astra", CredentialMode: "cookie", Verified: true,
		AccountBinding: openAICodexTicketAccountBinding(account), CapturedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute),
		Cookies: []*http.Cookie{{Name: "session", Value: "private", Path: "/backend-api", Secure: true, Expires: time.Now().Add(time.Second)}}}
	require.True(t, ticket.usable(time.Now(), account, cfg))
	require.False(t, ticket.usable(time.Now().Add(2*time.Second), account, cfg))
	stateCfg := cfg
	stateCfg.CredentialMode = "state"
	require.False(t, ticket.usable(time.Now(), account, stateCfg))
	stateCfg.CredentialMode = "cookie_state"
	require.False(t, ticket.usable(time.Now(), account, stateCfg))
	account.Extra = map[string]any{CodexTicketCredentialPolicyExtraKey: map[string]any{"mode": "state"}}
	require.False(t, ticket.usable(time.Now(), account, cfg))
}

func TestCodexTicketCookieSnapshotDoesNotExtendServerExpiry(t *testing.T) {
	jar := newOpenAICodexTicketCookieJar()
	req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	storeOpenAICodexTicketCookies(jar, req, &http.Response{Header: http.Header{"Set-Cookie": {
		"short=private; Path=/backend-api; Secure; Max-Age=2",
		"long=private; Path=/backend-api; Secure; Max-Age=600",
		"bad=private; Domain=other.example; Path=/",
	}}})
	cookies, expires := snapshotOpenAICodexTicketCookies(jar, 20*time.Second)
	require.Len(t, cookies, 2)
	require.LessOrEqual(t, time.Until(expires), 2*time.Second)
	for _, cookie := range cookies {
		require.Zero(t, cookie.MaxAge)
		require.False(t, cookie.Expires.IsZero())
	}
	storeOpenAICodexTicketCookies(jar, req, &http.Response{Header: http.Header{"Set-Cookie": {"short=; Path=/backend-api; Max-Age=-1"}}})
	cookies, _ = snapshotOpenAICodexTicketCookies(jar, 20*time.Second)
	require.Len(t, cookies, 1)
	require.Equal(t, "long", cookies[0].Name)
}

func TestCodexTicketCookieLifetimeStartsWhenCookieArrives(t *testing.T) {
	jar := newOpenAICodexTicketCookieJar().(*codexTicketCookieJar)
	req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	storeOpenAICodexTicketCookies(jar, req, &http.Response{Header: http.Header{"Set-Cookie": {"session=fresh; Path=/backend-api; Secure"}}})
	cookies, captured, expires := snapshotCodexTicketCookieLifetime(jar, 20*time.Second)
	require.Len(t, cookies, 1)
	require.WithinDuration(t, time.Now(), captured, time.Second)
	require.Equal(t, captured.Add(20*time.Second), expires)
	account := ticketTestAccount(41)
	cfg := resolveCodexTicketCredentialConfig(account, config.OpenAICodexTicketConfig{CredentialMode: "cookie"})
	ticket := &openAICodexTicket{CredentialMode: "cookie", Cookies: cookies, CapturedAt: captured, ExpiresAt: expires, Verified: true}
	require.True(t, ticket.usable(captured.Add(19*time.Second), account, cfg))
	require.False(t, ticket.usable(captured.Add(20*time.Second), account, cfg))
}

func TestCodexTicketCookieAccountOverrideAndFailureRelease(t *testing.T) {
	for _, result := range []string{"success", "no_cookie", "wrong_model"} {
		t.Run(result, func(t *testing.T) {
			calls := 0
			upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				calls++
				model := "gpt-6-astra"
				if result == "wrong_model" && calls == 2 {
					model = "gpt-5.6-luna"
				}
				response := codexTicketCompletedResponse(model, "")
				if result != "no_cookie" {
					response.Header.Add("Set-Cookie", "session=private; Path=/backend-api; Secure")
				}
				return response, nil
			}}
			cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, HarvestProxyURL: "http://harvest.example:8080"}
			svc, account := ticketTestService(t, cfg, upstream), ticketTestAccount(41)
			account.Extra = map[string]any{CodexTicketCredentialPolicyExtraKey: map[string]any{"mode": "cookie", "ttl_seconds": 30, "refresh_before_seconds": 2}}
			svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
			headers := make(http.Header)
			err := svc.applyOpenAICodexTicket(context.Background(), account, "gpt-6-astra", headers)
			if result != "success" {
				require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
				require.True(t, svc.openAICodexTicketBlocksAccount(account, "gpt-6-astra"))
				require.Empty(t, headers.Get("Cookie"))
				return
			}
			require.NoError(t, err)
			require.False(t, svc.openAICodexTicketBlocksAccount(account, "gpt-6-astra"))
			require.Equal(t, "session=private", headers.Get("Cookie"))
			ticket := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
			require.Equal(t, 30*time.Second, ticket.ExpiresAt.Sub(ticket.CapturedAt))
			ticket.ExpiresAt = time.Now().Add(-time.Second)
			svc.openaiCodexTickets.Store(openAICodexTicketKey(account.ID, ticket.Model), ticket)
			headers = make(http.Header)
			require.ErrorIs(t, svc.applyOpenAICodexTicket(context.Background(), account, ticket.Model, headers), ErrOpenAICodexTicketUnavailable)
			require.Empty(t, headers.Get("Cookie"))
		})
	}
}

func TestCodexTicketCookieShorterPolicyRefreshesAndReportsSameDeadline(t *testing.T) {
	account := ticketTestAccount(41)
	cfg := config.OpenAICodexTicketConfig{CredentialMode: "cookie", CookieTTLSeconds: 10, PoolCapacity: 1}
	start := time.Now().Add(-6 * time.Second)
	ticket := &openAICodexTicket{AccountID: 41, Model: "gpt-6-astra", CredentialMode: "cookie", Verified: true,
		CapturedAt: start, ExpiresAt: start.Add(time.Minute), Cookies: []*http.Cookie{{Name: "session", Value: "private", Path: "/", Expires: start.Add(time.Minute)}}}
	require.True(t, codexTicketPoolNeedsRefresh(ticket, account, cfg, time.Now()))
	status := codexTicketPoolStatus(ticket.Model, ticket, account, cfg, time.Now())
	require.True(t, status.Ready)
	require.Equal(t, start.Add(time.Minute), *status.ExpiresAt)
	require.GreaterOrEqual(t, status.RemainingSeconds, int64(50))
}

func TestCodexTicketCookieSharedCapacityExcludesMissingTicket(t *testing.T) {
	for _, override := range []bool{false, true} {
		account := ticketTestAccount(41)
		account.Status = StatusActive
		cfg := config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true, CredentialMode: "cookie", Models: []string{"gpt-6-astra"}}
		if override {
			cfg.CredentialMode = "state"
			account.Extra = map[string]any{CodexTicketCredentialPolicyExtraKey: map[string]any{"mode": "cookie"}}
		}
		snapshot := NewSharedPoolTicketAccountSnapshot(account, time.Now())
		snapshot.Available, snapshot.Concurrency = true, 2
		capacity := &SharedPoolCapacity{AvailableAccounts: 1, ConcurrencyCapacity: 2, TicketAccounts: []SharedPoolTicketAccountSnapshot{*snapshot}}
		result := GetSharedPoolCatalogCapacity(capacity, cfg, time.Now())
		require.Zero(t, result.AvailableAccounts)
		require.Zero(t, result.ConcurrencyCapacity)
		cfg.FailClosed = false
		result = GetSharedPoolCatalogCapacity(capacity, cfg, time.Now())
		require.Equal(t, capacity.AvailableAccounts, result.AvailableAccounts)
		require.Equal(t, capacity.ConcurrencyCapacity, result.ConcurrencyCapacity)
	}
}

func TestCodexTicketCookieSnapshotRejectsOversizedSets(t *testing.T) {
	for _, test := range []struct {
		name        string
		count, size int
		accepted    bool
	}{{"at_limit", 32, 10, true}, {"too_many", 33, 10, false}, {"too_large", 2, 4096, false}} {
		t.Run(test.name, func(t *testing.T) {
			jar := newOpenAICodexTicketCookieJar()
			req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
			require.NoError(t, err)
			var values []*http.Cookie
			for i := 0; i < test.count; i++ {
				values = append(values, &http.Cookie{Name: fmt.Sprintf("cookie%d", i), Value: strings.Repeat("v", test.size), Path: "/", Secure: true})
			}
			jar.SetCookies(req.URL, values)
			cookies, captured, expires := snapshotCodexTicketCookieLifetime(jar, 20*time.Second)
			if test.accepted {
				require.Len(t, cookies, test.count)
				return
			}
			require.Empty(t, cookies)
			require.True(t, captured.IsZero())
			require.True(t, expires.IsZero())
		})
	}
}
