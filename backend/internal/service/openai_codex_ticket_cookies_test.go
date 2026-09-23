package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestCodexTicketCookiesReachVerificationAndRemainAttemptScoped(t *testing.T) {
	calls := 0
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		calls++
		if calls == 2 {
			require.Equal(t, "ticket_session=probe-private", req.Header.Get("Cookie"))
		} else {
			require.Empty(t, req.Header.Get("Cookie"))
		}
		resp := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(292))
		resp.Header.Add("Set-Cookie", "ticket_session=probe-private; Domain=chatgpt.com; Path=/backend-api; Secure; HttpOnly")
		return resp, nil
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{}, upstream)
	in := openAICodexTicketProbeInput{Account: ticketTestAccount(41), Token: "test-token", Model: "gpt-6-astra",
		Timeout: time.Second, Attempt: newCodexTicketAttempt("gpt-6-astra"), CookieJar: newOpenAICodexTicketCookieJar()}
	state, _, err := svc.probeOpenAICodexTicket(context.Background(), in)
	require.NoError(t, err)
	in.State = state
	_, _, err = svc.probeOpenAICodexTicket(context.Background(), in)
	require.NoError(t, err)
	encoded, err := json.Marshal(in.Attempt)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "probe-private")
	require.Equal(t, []string{"[REDACTED]"}, in.Attempt.BusinessExchange.Request.Headers["Cookie"])
	require.Equal(t, []string{"[REDACTED]"}, in.Attempt.HarvestExchange.Response.Headers["Set-Cookie"])
	for _, accountID := range []int64{41, 42} {
		in.Account, in.State = ticketTestAccount(accountID), ""
		in.CookieJar = newOpenAICodexTicketCookieJar()
		_, _, err = svc.probeOpenAICodexTicket(context.Background(), in)
		require.NoError(t, err)
	}
	require.Equal(t, 4, calls)
}

func TestCodexTicketCookiesUseDomainPathAndSecureScope(t *testing.T) {
	jar := newOpenAICodexTicketCookieJar()
	request, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	response := &http.Response{Header: http.Header{"Set-Cookie": {
		"valid=probe-private; Domain=chatgpt.com; Path=/backend-api; Secure",
		"host_only=host-private; Path=/backend-api; Secure",
		"unrelated=wrong-private; Domain=other.example; Path=/",
		"public_suffix=wrong-private; Domain=com; Path=/",
		"expired=old-private; Path=/; Max-Age=-1",
	}}}
	storeOpenAICodexTicketCookies(jar, request, response)
	for _, tc := range []struct {
		target string
		names  []string
	}{
		{chatgptCodexURL, []string{"valid", "host_only"}},
		{"https://chatgpt.com/backend-api/codex/next", []string{"valid", "host_only"}},
		{"https://chatgpt.com/backend-api-other", nil},
		{"https://chatgpt.com/other", nil},
		{"https://api.chatgpt.com/backend-api/codex/responses", []string{"valid"}},
		{"http://chatgpt.com/backend-api/codex/responses", nil},
		{"https://other.example/backend-api/codex/responses", nil},
		{"https://chatgpt.com.evil.example/backend-api/codex/responses", nil},
	} {
		t.Run(tc.target, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, tc.target, nil)
			require.NoError(t, err)
			applyOpenAICodexTicketCookies(jar, req)
			var names []string
			for _, cookie := range req.Cookies() {
				names = append(names, cookie.Name)
			}
			require.ElementsMatch(t, tc.names, names)
		})
	}
	response.Header.Set("Set-Cookie", "valid=; Domain=chatgpt.com; Path=/backend-api; Max-Age=-1")
	storeOpenAICodexTicketCookies(jar, request, response)
	remaining := jar.Cookies(request.URL)
	require.Len(t, remaining, 1)
	require.Equal(t, "host_only", remaining[0].Name)
}

func TestCodexTicketCookieJarCreatedForEachHarvest(t *testing.T) {
	calls := 0
	upstream := &codexTicketFuncUpstream{do: func(req *http.Request) (*http.Response, error) {
		calls++
		if calls%2 == 1 {
			require.Empty(t, req.Header.Get("Cookie"))
		} else {
			require.Equal(t, "stage=collection-private", req.Header.Get("Cookie"))
		}
		resp := codexTicketCompletedResponse("gpt-6-astra", fakeCodexTicketState(292))
		resp.Header.Add("Set-Cookie", "stage=collection-private; Path=/backend-api; Secure")
		return resp, nil
	}}
	svc := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestProxyURL: "http://harvest.example:8080"}, upstream)
	for _, accountID := range []int64{41, 42} {
		svc.probeOnceOpenAICodexTicket(context.Background(), ticketTestAccount(accountID), "gpt-6-astra")
	}
	require.Equal(t, 4, calls)
}

func TestCodexTicketCookieStorageUsesRequestOrigin(t *testing.T) {
	jar := newOpenAICodexTicketCookieJar()
	req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	unrelated, err := url.Parse("https://other.example/backend-api/codex/responses")
	require.NoError(t, err)
	resp := &http.Response{Request: &http.Request{URL: unrelated}, Header: http.Header{"Set-Cookie": {"session=probe-private; Path=/; Secure"}}}
	storeOpenAICodexTicketCookies(jar, req, resp)
	require.Empty(t, jar.Cookies(unrelated))
	require.Len(t, jar.Cookies(req.URL), 1)
}

func TestCodexTicketCookieCandidateKeepsPublishedJarAndAccepts312(t *testing.T) {
	jar := newOpenAICodexTicketCookieJar()
	req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	storeOpenAICodexTicketCookies(jar, req, &http.Response{Header: http.Header{"Set-Cookie": {
		"session=old; Path=/backend-api; Secure; Max-Age=60",
	}}})
	before := codexTicketCookieJarRevision(jar)
	candidate, changed := candidateOpenAICodexTicketCookies(jar, req, &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"X-Codex-Turn-State": {fakeCodexTicketState(312)},
			"Set-Cookie":         {"session=new; Path=/backend-api; Secure; Max-Age=60"},
		},
	}, time.Minute)
	require.True(t, changed)
	require.NotNil(t, candidate)
	require.True(t, candidate.HeaderChanged)
	require.Greater(t, candidate.Revision, before)
	require.Equal(t, "session=old", jar.Cookies(req.URL)[0].Name+"="+jar.Cookies(req.URL)[0].Value)
	require.Equal(t, "session=new", candidate.Jar.Cookies(req.URL)[0].Name+"="+candidate.Jar.Cookies(req.URL)[0].Value)
	require.Equal(t, before, codexTicketCookieJarRevision(jar))
}

func TestCodexTicketCookieCandidateDetectsAddedAndDeletedCookies(t *testing.T) {
	jar := newOpenAICodexTicketCookieJar()
	req, err := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
	require.NoError(t, err)
	storeOpenAICodexTicketCookies(jar, req, &http.Response{Header: http.Header{"Set-Cookie": {
		"session=old; Path=/backend-api; Secure; Max-Age=60",
	}}})
	added, changed := candidateOpenAICodexTicketCookies(jar, req, &http.Response{Header: http.Header{"Set-Cookie": {
		"csrf=token; Path=/backend-api; Secure; Max-Age=60",
	}}}, time.Minute)
	require.True(t, changed)
	require.True(t, added.HeaderChanged)
	require.Len(t, added.Cookies, 2)
	deleted, changed := candidateOpenAICodexTicketCookies(jar, req, &http.Response{Header: http.Header{"Set-Cookie": {
		"session=; Path=/backend-api; Max-Age=-1",
	}}}, time.Minute)
	require.True(t, changed)
	require.NotNil(t, deleted)
	require.True(t, deleted.HeaderChanged)
	require.Empty(t, deleted.Cookies)
	require.Len(t, jar.Cookies(req.URL), 1)
}

func TestCodexTicketCookieJarRejectsNilAndBoundsMaxAge(t *testing.T) {
	u, err := url.Parse(chatgptCodexURL)
	require.NoError(t, err)
	jar := newOpenAICodexTicketCookieJar()
	require.NotPanics(t, func() {
		jar.SetCookies(nil, []*http.Cookie{nil})
		jar.SetCookies(u, []*http.Cookie{nil, {Name: "invalid name", Value: "bad"}})
	})
	require.Zero(t, codexTicketCookieJarRevision(jar))
	value := &http.Cookie{Name: "session", Value: "valid", Path: "/", MaxAge: int(^uint(0) >> 1),
		Expires: time.Now().Add(-time.Hour)}
	jar.SetCookies(u, []*http.Cookie{value})
	cookies, _, expires := snapshotCodexTicketCookieLifetime(jar, time.Second)
	require.Len(t, cookies, 1)
	require.True(t, expires.After(time.Now().Add(24*time.Hour)), "Max-Age 必须优先且不能溢出为过去")
	require.Equal(t, expires, cookies[0].Expires)
	require.Zero(t, cookies[0].MaxAge)
	require.Greater(t, value.MaxAge, 0, "输入 Cookie 不可原地修改")
}

func TestCodexTicketCookieClonePreservesHostScopeOrderAndLifetime(t *testing.T) {
	u, _ := url.Parse(chatgptCodexURL)
	jar := newOpenAICodexTicketCookieJar()
	jar.SetCookies(u, []*http.Cookie{
		{Name: "z", Value: "host", Path: "/", Secure: true},
		{Name: "a", Value: "domain", Domain: ".chatgpt.com", Path: "/", Secure: true},
	})
	cloned := cloneOpenAICodexTicketCookieJar(jar)
	require.Equal(t, "z=host; a=domain", codexTicketCookieHeaderValues(cloned.Cookies(u)))
	subdomain, _ := url.Parse("https://api.chatgpt.com/")
	require.Equal(t, "a=domain", codexTicketCookieHeaderValues(cloned.Cookies(subdomain)))
	before, captured, expires := snapshotCodexTicketCookieLifetime(jar, time.Minute)
	after, clonedCaptured, clonedExpires := snapshotCodexTicketCookieLifetime(cloned, time.Minute)
	require.Equal(t, before, after)
	require.Equal(t, captured, clonedCaptured)
	require.Equal(t, expires, clonedExpires)
	cloned.SetCookies(u, []*http.Cookie{{Name: "z", Value: "replacement", Path: "/", Secure: true}})
	require.Equal(t, "z=host; a=domain", codexTicketCookieHeaderValues(jar.Cookies(u)))
}

func TestCodexTicketCookieSameValueRenewalDoesNotChangeHeaders(t *testing.T) {
	for _, attributes := range []string{"Max-Age=60", "Expires=" + time.Now().Add(time.Hour).UTC().Format(http.TimeFormat), ""} {
		t.Run(attributes, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodPost, chatgptCodexURL, nil)
			response := &http.Response{Header: http.Header{"Set-Cookie": {"session=same; Path=/; Secure; " + attributes}}}
			original := newOpenAICodexTicketCookieJar()
			storeOpenAICodexTicketCookies(original, req, response)
			jar := original
			for range 4 {
				previousRevision := codexTicketCookieJarRevision(jar)
				candidate, changed := candidateOpenAICodexTicketCookies(jar, req, response, time.Minute)
				require.True(t, changed, "同值续期每次均需形成候选，不依赖时钟分辨率")
				require.False(t, candidate.HeaderChanged, "有效期变化不应要求无限次更换凭据复验")
				require.Equal(t, previousRevision+1, candidate.Revision)
				jar = candidate.Jar
			}
			require.Equal(t, uint64(1), codexTicketCookieJarRevision(original))
		})
	}
}

func TestCodexTicketCookieSnapshotRestoresSessionFallbackWithoutRenewal(t *testing.T) {
	captured := time.Now().Add(-5 * time.Second)
	hard := captured.Add(10 * time.Minute)
	cookies := []*http.Cookie{
		{Name: "session", Value: "short", Path: "/", Secure: true, Expires: captured.Add(20 * time.Second)},
		{Name: "persistent", Value: "long", Path: "/", Secure: true, Expires: hard},
	}
	keys := []string{codexTicketCookieKey(*cookies[0])}
	jar := newOpenAICodexTicketCookieJarFromSnapshot(cookies, captured, keys)
	values, at, expires := snapshotCodexTicketCookieLifetime(jar, 20*time.Second)
	require.Len(t, values, 2)
	require.Equal(t, captured, at)
	require.Equal(t, captured.Add(20*time.Second), expires)
	require.Equal(t, keys, snapshotCodexTicketCookieSessionKeys(jar))
	require.Equal(t, hard, snapshotCodexTicketCookieHardExpiresAt(jar))
	values[0].Value = "modified"
	require.Equal(t, "short", cookies[0].Value)
	require.Equal(t, captured.Add(20*time.Second), cookies[0].Expires)
	renewed := newOpenAICodexTicketCookieJarFromSnapshot(cookies, captured.Add(time.Second), keys)
	_, _, renewedExpiry := snapshotCodexTicketCookieLifetime(renewed, 20*time.Second)
	require.Equal(t, expires.Add(time.Second), renewedExpiry)
	require.Equal(t, hard, snapshotCodexTicketCookieHardExpiresAt(renewed))
}

func TestCodexTicketCookieSessionMetadataDoesNotInventHardExpiry(t *testing.T) {
	cookie := &http.Cookie{Name: "session", Value: "value", Path: "/", Secure: true}
	jar := newOpenAICodexTicketCookieJarFromSnapshot([]*http.Cookie{cookie}, time.Now(), nil)
	require.Equal(t, []string{codexTicketCookieKey(*cookie)}, snapshotCodexTicketCookieSessionKeys(jar))
	require.True(t, snapshotCodexTicketCookieHardExpiresAt(jar).IsZero())
	_, _, fallback := snapshotCodexTicketCookieLifetime(jar, time.Minute)
	require.True(t, fallback.After(time.Now()))
}
