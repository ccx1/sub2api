package service

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketCookieVerificationDoesNotPublishResponseReplacement(t *testing.T) {
	for _, mode := range []string{"cookie", "cookie_state"} {
		for _, replacement := range []string{"session=replaced; Max-Age=60", "session=original; Max-Age=1", "session=; Max-Age=0"} {
			t.Run(mode+"/"+replacement, func(t *testing.T) {
				calls := 0
				upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
					calls++
					response := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(292))
					cookie := "session=original; Max-Age=60"
					if calls == 2 {
						require.Equal(t, "session=original", req.Header.Get("Cookie"))
						cookie = replacement
					}
					response.Header.Add("Set-Cookie", cookie+"; Path=/backend-api; Secure")
					return response, nil
				}}
				cfg := config.OpenAICodexTicketConfig{Enabled: true, CredentialMode: mode, HarvestProxyURL: "http://harvest.example:8080"}
				svc, account := ticketTestService(t, cfg, upstream), ticketTestAccount(41)
				svc.probeOnceOpenAICodexTicket(context.Background(), account, "gpt-6-astra")
				wantCalls := 3
				if strings.Contains(replacement, "Max-Age=0") || strings.Contains(replacement, "session=original") {
					wantCalls = 2
				}
				require.Equal(t, wantCalls, calls, "changed Cookie is reverified before publication")
				published := svc.lookupOpenAICodexTicket(account, "gpt-6-astra")
				if strings.Contains(replacement, "session=original") && strings.Contains(replacement, "Max-Age=1") {
					require.NotNil(t, published, "unchanged credential values were verified by the same completed request")
					require.WithinDuration(t, time.Now().Add(time.Second), published.ExpiresAt, time.Second)
				} else {
					require.Nil(t, published)
				}
				require.False(t, svc.openAICodexTicketBlocksAccount(account, "gpt-6-astra"))
			})
		}
	}
}

func TestCodexTicketCookieVerificationKeepsOriginalLifetime(t *testing.T) {
	for _, age := range []time.Duration{8 * time.Second, 21 * time.Second} {
		t.Run(age.String(), func(t *testing.T) {
			account := ticketTestAccount(41)
			cfg := resolveCodexTicketCredentialConfig(account, config.OpenAICodexTicketConfig{Enabled: true, CredentialMode: "cookie"})
			jar := newOpenAICodexTicketCookieJar().(*codexTicketCookieJar)
			req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
			require.NoError(t, err)
			jar.SetCookies(req.URL, []*http.Cookie{{Name: "session", Value: "original", Path: "/", Secure: true}})
			captured := time.Now().Add(-age)
			for key, entry := range jar.entries {
				entry.at = captured
				jar.entries[key] = entry
			}
			calls := 0
			svc := ticketTestService(t, cfg, &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "session=original", req.Header.Get("Cookie"))
				response := codexTicketCompletedResponse("gpt-6-astra", "")
				response.Header.Add("Set-Cookie", "session=original; Max-Age=600; Path=/; Secure")
				return response, nil
			}})
			_, _, err = svc.probeOpenAICodexTicket(context.Background(), openAICodexTicketProbeInput{
				Account: account, Token: "test", Model: "gpt-6-astra", Config: &cfg, CookieJar: jar,
				BusinessVerification: true, Timeout: time.Second})
			if age > 20*time.Second {
				require.Error(t, err)
				require.Zero(t, calls)
				return
			}
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			_, after, expires := snapshotCodexTicketCookieLifetime(jar, 20*time.Second)
			require.Equal(t, captured, after)
			require.Equal(t, captured.Add(20*time.Second), expires)
		})
	}
}

func TestCodexTicketProbeStoresNon200CookieInCandidateOnly(t *testing.T) {
	jar := newOpenAICodexTicketCookieJar()
	req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	storeOpenAICodexTicketCookies(jar, req, &http.Response{Header: http.Header{"Set-Cookie": {
		"session=old; Path=/backend-api; Secure; Max-Age=60",
	}}})
	upstream := &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusPermanentRedirect,
			Header: http.Header{"Set-Cookie": {"session=new; Path=/backend-api; Secure; Max-Age=60"}},
			Body:   http.NoBody}, nil
	}}
	cfg := config.OpenAICodexTicketConfig{Enabled: true, CredentialMode: config.CodexTicketCredentialCookie}
	svc := ticketTestService(t, cfg, upstream)
	candidate := &openAICodexTicketCookieCandidate{}
	_, status, err := svc.probeOpenAICodexTicket(context.Background(), openAICodexTicketProbeInput{
		Account: ticketTestAccount(41), Token: "test", Model: "gpt-6-astra", Config: &cfg,
		CookieJar: jar, CookieCandidate: candidate, BusinessVerification: true, Timeout: time.Second,
	})
	require.Error(t, err)
	require.Equal(t, http.StatusPermanentRedirect, status)
	require.NotNil(t, candidate.Jar)
	require.Equal(t, "old", jar.Cookies(req.URL)[0].Value)
	require.Equal(t, "new", candidate.Jar.Cookies(req.URL)[0].Value)
}
