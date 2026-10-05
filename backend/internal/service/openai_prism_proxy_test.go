package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrismBrowserEgressAckMatchesAdapterVector(t *testing.T) {
	require.Equal(t, "821c7aeee10ea57403b226156b20e94330ba1c83cd6fedbe879eec33821950a1",
		prismBrowserEgressAck("key", strings.Repeat("a", 64), 300, "http://proxy.example:80"))
}

func TestPrismBrowserProxyURLUsesResolvedEgress(t *testing.T) {
	for _, protocol := range []string{"http", "https", "socks5", "socks5h"} {
		t.Run(protocol, func(t *testing.T) {
			proxy := &Proxy{Protocol: protocol, Host: "2001:db8::1", Port: 8080}
			account := randomProxyAccount(RandomProxyEmptyPoolPolicyReject)
			account.Proxy = proxy
			got, err := prismBrowserProxyURL(account)
			require.NoError(t, err)
			require.Equal(t, proxy.URL(), got)
		})
	}
	account := randomProxyAccount(RandomProxyEmptyPoolPolicyDirect)
	_, err := prismBrowserProxyURL(account)
	require.Error(t, err, "a policy alone does not prove the proxy pool was resolved")
	require.NoError(t, ResolveRandomProxy(context.Background(), account, nil))
	got, err := prismBrowserProxyURL(account)
	require.NoError(t, err)
	require.Equal(t, "direct", got)
	for _, policy := range []string{RandomProxyEmptyPoolPolicyReject, RandomProxyEmptyPoolPolicyDisable} {
		_, err = prismBrowserProxyURL(randomProxyAccount(policy))
		require.Error(t, err)
	}
	_, err = prismBrowserProxyURL(&Account{ProxyID: new(int64(7))})
	require.Error(t, err)
}

func TestPrismBrowserProxyURLRejectsUnsupportedWithoutSecrets(t *testing.T) {
	for _, proxy := range []*Proxy{
		{Protocol: "file", Host: "private-host", Port: 8080},
		{Protocol: "http", Host: "private-host", Port: 0},
		{Protocol: "https", Host: "private-host/path", Port: 8080},
		{Protocol: "socks5", Host: "private-host", Port: 1080, Username: "private-user", Password: "private-pass"},
		{Protocol: "socks5h", Host: "private-host", Port: 1080, Username: "private-user", Password: "private-pass"},
	} {
		_, err := prismBrowserProxyURL(&Account{Proxy: proxy})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "private-")
	}
}

func TestPrismBrowserProxyForwardStaysOnLoopback(t *testing.T) {
	var proxyHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHeader = r.Header.Get("X-Prism-Proxy-URL")
		if proxyHeader != "direct" {
			nonce := r.Header.Get("X-Prism-Egress-Nonce")
			require.Len(t, nonce, 64)
			if r.Method == http.MethodGet {
				require.Empty(t, r.Header.Get("X-Prism-OAuth-Token"))
				nonce = "probe:" + nonce
			}
			w.Header().Set("X-Prism-Egress-Ack", prismBrowserEgressAck("fixture-bridge-key", nonce, 42, proxyHeader))
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	s, account := prismTestService(server.URL)
	account.Proxy = &Proxy{Protocol: "https", Host: "not-reachable.invalid", Port: 8443,
		Username: "fixture user", Password: "fixture/@secret"}
	_, _, status, err := s.callPrismBrowser(context.Background(), account, []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, account.Proxy.URL(), proxyHeader)
	account.Proxy = nil
	_, _, _, err = s.callPrismBrowser(context.Background(), account, []byte(`{}`))
	require.NoError(t, err)
	require.Equal(t, "direct", proxyHeader)
}

func TestPrismBrowserProxyRejectsMissingForgedAndReplayedAck(t *testing.T) {
	var previous string
	mode := "valid"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("X-Prism-Egress-Ack", prismBrowserEgressAck("fixture-bridge-key", "probe:"+r.Header.Get("X-Prism-Egress-Nonce"), 42, r.Header.Get("X-Prism-Proxy-URL")))
			return
		}
		ack := prismBrowserEgressAck("fixture-bridge-key", r.Header.Get("X-Prism-Egress-Nonce"), 42, r.Header.Get("X-Prism-Proxy-URL"))
		switch mode {
		case "valid":
			previous = ack
			w.Header().Set("X-Prism-Egress-Ack", ack)
		case "replayed":
			w.Header().Set("X-Prism-Egress-Ack", previous)
		case "forged":
			w.Header().Set("X-Prism-Egress-Ack", "fixture-forged")
		case "duplicate":
			w.Header().Add("X-Prism-Egress-Ack", ack)
			w.Header().Add("X-Prism-Egress-Ack", ack)
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	defer server.Close()
	s, account := prismTestService(server.URL)
	account.Proxy = &Proxy{Protocol: "http", Host: "proxy.invalid", Port: 8080}
	_, _, _, err := s.callPrismBrowser(context.Background(), account, []byte(`{}`))
	require.NoError(t, err)
	for _, next := range []string{"missing", "replayed", "forged", "duplicate"} {
		mode = next
		_, _, _, err = s.callPrismBrowser(context.Background(), account, []byte(`{}`))
		require.ErrorContains(t, err, "did not confirm")
		require.NotContains(t, err.Error(), account.Proxy.Host)
	}
}

func TestPrismBrowserProxyOldAdapterRejectedBeforeSubmission(t *testing.T) {
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	defer server.Close()
	s, account := prismTestService(server.URL)
	account.Proxy = &Proxy{Protocol: "http", Host: "proxy.invalid", Port: 8080}
	_, _, _, err := s.callPrismBrowser(context.Background(), account, []byte(`{}`))
	require.ErrorContains(t, err, "model request was not submitted")
	require.Zero(t, posts)
}
