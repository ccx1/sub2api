package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/stretchr/testify/require"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type resetAccountStub struct{ account *Account }

func (s resetAccountStub) GetByID(context.Context, int64) (*Account, error) { return s.account, nil }

type resetTokenStub struct{}

func (resetTokenStub) GetAccessToken(context.Context, *Account) (string, error) {
	return "synthetic-token", nil
}
func TestClaudeResetCreditStatusNativeContract(t *testing.T) {
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	s := &ClaudeResetCreditService{accounts: resetAccountStub{&Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"scope": "user:profile user:inference"}}}, tokens: resetTokenStub{}, now: func() time.Time { return now }}
	s.do = func(r *http.Request, p string) (*http.Response, error) {
		require.Equal(t, claudeResetUsageURL, r.URL.String())
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "Bearer synthetic-token", r.Header.Get("Authorization"))
		require.Contains(t, r.Header.Get("User-Agent"), "claude-cli/")
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"cedar_ember":{"eligible":true,"at_limit":false,"next_grant_id":"launch","grants":[{"id":"launch","resets_left":2,"usable_now":true,"use_requires_limit":false,"ends_at":"2026-10-22T00:00:00Z","clears":["five_hour"],"percent_used":{"five_hour":65},"blocking":[]},{"id":"later","clears":["five_hour"],"resets_left":1,"usable_now":true,"use_requires_limit":false}]}}`))}, nil
	}
	r, e := s.Query(context.Background(), 1)
	require.NoError(t, e)
	require.Equal(t, 2, r.AvailableCount)
	require.True(t, r.Credits[0].Redeemable)
	require.False(t, r.Credits[1].Redeemable)
	b, e := json.Marshal(r)
	require.NoError(t, e)
	require.NotContains(t, string(b), `"id"`)
	require.NotContains(t, string(b), "launch")
	require.NotContains(t, string(b), "synthetic-token")
	require.NotContains(t, string(b), "selection_token")
}
func TestClaudeResetCreditPastCooldownIsCleared(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Minute)
	no := false
	g := claudeResetGrant{ID: "grant", ResetsLeft: 1, UsableNow: true, UseRequiresLimit: &no, Clears: []string{"five_hour"}}
	r := projectClaudeResetCredits(&claudeResetBlock{Eligible: true, NextGrantID: g.ID, CooldownUntil: &past, Grants: []claudeResetGrant{g}}, now)
	require.Nil(t, r.CooldownUntil)
	require.Equal(t, 1, r.AvailableCount)
	future := now.Add(time.Hour)
	r = projectClaudeResetCredits(&claudeResetBlock{Eligible: true, NextGrantID: g.ID, CooldownUntil: &future, Grants: []claudeResetGrant{g}}, now)
	require.Equal(t, &future, r.CooldownUntil)
}
func TestClaudeResetCreditEligibilityFailClosed(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Minute)
	future := now.Add(time.Hour)
	no := false
	base := claudeResetGrant{ID: "grant", ResetsLeft: 1, UsableNow: true, UseRequiresLimit: &no, Clears: []string{"five_hour"}}
	for _, name := range []string{"paused", "expired", "future", "cooldown", "requires-limit", "blocking", "ineligible", "spent", "not-next"} {
		t.Run(name, func(t *testing.T) {
			g := base
			b := &claudeResetBlock{Eligible: true, NextGrantID: g.ID}
			switch name {
			case "paused":
				g.Paused = true
			case "expired":
				g.EndsAt = &past
			case "future":
				g.StartsAt = &future
			case "cooldown":
				b.CooldownUntil = &future
			case "requires-limit":
				g.UseRequiresLimit = nil
			case "blocking":
				g.Blocking = []string{"seven_day"}
			case "ineligible":
				b.Eligible = false
			case "spent":
				g.ResetsLeft = 0
			case "not-next":
				b.NextGrantID = "other"
			}
			b.Grants = []claudeResetGrant{g}
			r := projectClaudeResetCredits(b, now)
			require.Zero(t, r.AvailableCount)
		})
	}
}
func TestClaudeResetCreditStatusRejectsMissingScopeBeforeNetwork(t *testing.T) {
	s := &ClaudeResetCreditService{accounts: resetAccountStub{&Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}}, do: func(*http.Request, string) (*http.Response, error) { t.Fatal("network called"); return nil, nil }}
	_, e := s.Query(context.Background(), 1)
	require.Error(t, e)
}
func TestClaudeResetCreditMalformedAndAbsent(t *testing.T) {
	for _, body := range []string{`{}`, `{"five_hour":{},"cedar_ember":null}`, `{"cedar_ember":{"eligible":true}}`, `{"error":{"message":"private upstream data"}}`, `not json`} {
		t.Run(body, func(t *testing.T) {
			s := &ClaudeResetCreditService{accounts: resetAccountStub{&Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"scope": "user:profile"}}}, tokens: resetTokenStub{}, now: time.Now, do: func(*http.Request, string) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
			}}
			r, e := s.Query(context.Background(), 1)
			if body == `{}` || strings.Contains(body, `"cedar_ember":null`) {
				require.NoError(t, e)
				require.Empty(t, r.Credits)
			} else {
				require.Error(t, e)
				require.NotContains(t, e.Error(), "private upstream data")
			}
		})
	}
}

type resetProxyAccounts struct {
	resetAccountStub
	randomProxySelectorStub
	disabled   []int64
	disableErr error
}

func (s *resetProxyAccounts) DisableRandomProxyAccountIfUnavailable(_ context.Context, id int64) error {
	s.disabled = append(s.disabled, id)
	return s.disableErr
}

type resetProxyRepository struct {
	ProxyRepository
	proxy *Proxy
	err   error
}

func (s *resetProxyRepository) GetByID(context.Context, int64) (*Proxy, error) {
	return s.proxy, s.err
}

func resetProxyTestAccount() *Account {
	return &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"scope": "user:profile"}}
}

func resetProxyTestService(accounts claudeResetAccounts) *ClaudeResetCreditService {
	return &ClaudeResetCreditService{accounts: accounts, tokens: resetTokenStub{}, now: time.Now}
}

func TestClaudeResetCreditRandomProxyUsesRequestCopy(t *testing.T) {
	account := resetProxyTestAccount()
	account.Extra = map[string]any{ProxyModeExtraKey: ProxyModeRandom}
	stale := &Proxy{ID: 3, Status: StatusActive}
	account.ProxyID, account.Proxy = &stale.ID, stale
	selected := &Proxy{ID: 7, Protocol: "http", Host: "127.0.0.1", Port: 8080, Status: StatusActive}
	repo := &resetProxyAccounts{resetAccountStub: resetAccountStub{account}, randomProxySelectorStub: randomProxySelectorStub{proxy: selected}}
	s := resetProxyTestService(repo)
	s.do = func(_ *http.Request, proxy string) (*http.Response, error) {
		require.Equal(t, selected.URL(), proxy)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	}
	_, err := s.Query(context.Background(), account.ID)
	require.NoError(t, err)
	require.Equal(t, 1, repo.calls)
	require.Equal(t, &stale.ID, account.ProxyID)
	require.Same(t, stale, account.Proxy)
}

func TestClaudeResetCreditRandomProxyFailurePolicies(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	for _, tc := range []struct {
		name, policy            string
		proxy                   *Proxy
		selectorErr, disableErr error
	}{
		{name: "reject", policy: RandomProxyEmptyPoolPolicyReject},
		{name: "disable", policy: RandomProxyEmptyPoolPolicyDisable},
		{name: "direct", policy: RandomProxyEmptyPoolPolicyDirect},
		{name: "selection-error", policy: RandomProxyEmptyPoolPolicyDirect, selectorErr: errors.New("pool failed")},
		{name: "inactive", policy: RandomProxyEmptyPoolPolicyReject, proxy: &Proxy{ID: 7, Status: StatusDisabled}},
		{name: "expired", policy: RandomProxyEmptyPoolPolicyReject, proxy: &Proxy{ID: 7, Status: StatusActive, ExpiresAt: &past}},
		{name: "disable-error", policy: RandomProxyEmptyPoolPolicyDisable, disableErr: errors.New("disable failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := resetProxyTestAccount()
			account.Extra = map[string]any{ProxyModeExtraKey: ProxyModeRandom, RandomProxyEmptyPoolPolicyExtraKey: tc.policy}
			repo := &resetProxyAccounts{resetAccountStub: resetAccountStub{account}, randomProxySelectorStub: randomProxySelectorStub{proxy: tc.proxy, err: tc.selectorErr}, disableErr: tc.disableErr}
			s := resetProxyTestService(repo)
			s.do = func(_ *http.Request, proxy string) (*http.Response, error) {
				require.Equal(t, "direct", tc.name)
				require.Empty(t, proxy)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			}
			_, err := s.Query(context.Background(), account.ID)
			if tc.name == "direct" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, tc.policy == RandomProxyEmptyPoolPolicyDisable, len(repo.disabled) == 1)
			require.Nil(t, account.ProxyID)
			require.Nil(t, account.Proxy)
		})
	}
}

func TestClaudeResetCreditFixedProxyNeverFallsBackToDirect(t *testing.T) {
	past := time.Now().Add(-time.Minute)
	for _, tc := range []struct {
		name  string
		proxy *Proxy
		err   error
	}{
		{name: "active", proxy: &Proxy{ID: 7, Protocol: "http", Host: "127.0.0.1", Port: 8080, Status: StatusActive}},
		{name: "missing"},
		{name: "lookup-error", err: errors.New("lookup failed")},
		{name: "inactive", proxy: &Proxy{ID: 7, Status: StatusDisabled}},
		{name: "expired", proxy: &Proxy{ID: 7, Status: StatusActive, ExpiresAt: &past}},
		{name: "wrong-id", proxy: &Proxy{ID: 8, Status: StatusActive}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := resetProxyTestAccount()
			id := int64(7)
			account.ProxyID = &id
			s := resetProxyTestService(resetAccountStub{account})
			s.proxies = &resetProxyRepository{proxy: tc.proxy, err: tc.err}
			s.do = func(_ *http.Request, proxy string) (*http.Response, error) {
				require.Equal(t, "active", tc.name)
				require.Equal(t, tc.proxy.URL(), proxy)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			}
			_, err := s.Query(context.Background(), account.ID)
			require.Equal(t, tc.name == "active", err == nil)
			require.Nil(t, account.Proxy)
		})
	}
}

func TestClaudeResetCreditRejectsNonOAuthAccountsBeforeNetwork(t *testing.T) {
	for _, account := range []*Account{{Platform: PlatformOpenAI, Type: AccountTypeOAuth}, {Platform: PlatformAnthropic, Type: AccountTypeAPIKey}, {Platform: PlatformAnthropic, Type: AccountTypeSetupToken}} {
		s := resetProxyTestService(resetAccountStub{account})
		s.do = func(*http.Request, string) (*http.Response, error) { t.Fatal("network called"); return nil, nil }
		_, err := s.Query(context.Background(), 1)
		require.Error(t, err)
	}
}
