package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

const ipfmSampleLocationHTML = `<pre class="px-4"><p class="block"><span class="has-text-grey-light">IP: </span>2a13:9500:124:0:9b3:e7f:cc93:82a5</p><p class="block"><span class="has-text-grey-light">Location: </span>Tokyo, Japan</p><p class="block"><span class="has-text-grey-light">AS Name: </span>Lumina broadband UAB</p></pre>`

const ipfmSampleNullLocationHTML = `<pre class="px-4"><p class="block"><span class="has-text-grey-light">IP: </span>1.1.1.1</p><p class="block"><span class="has-text-grey-light">Location: </span><span class="tag is-warning">null</span></p></pre>`

func withIPFMBaseURL(t *testing.T, baseURL string) {
	t.Helper()
	original := proxyQualityIPFMBaseURL
	proxyQualityIPFMBaseURL = baseURL
	t.Cleanup(func() { proxyQualityIPFMBaseURL = original })
}

func TestParseIPFMLocation(t *testing.T) {
	location, ok := parseIPFMLocation([]byte(ipfmSampleLocationHTML))
	require.True(t, ok)
	require.Equal(t, "Tokyo, Japan", location)

	location, ok = parseIPFMLocation([]byte(ipfmSampleNullLocationHTML))
	require.True(t, ok)
	require.Empty(t, location)

	_, ok = parseIPFMLocation([]byte(`<pre class="px-4"><p class="block">Not a valid IP address</p></pre>`))
	require.False(t, ok)
}

func TestRunProxyQualityIPFMLocation_Pass(t *testing.T) {
	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(ipfmSampleLocationHTML))
	}))
	defer server.Close()
	withIPFMBaseURL(t, server.URL+"/ip/")

	item := runProxyQualityIPFMLocation(context.Background(), server.Client(), "2a13:9500:124:0:9b3:e7f:cc93:82a5")
	require.Equal(t, proxyQualityIPFMTarget, item.Target)
	require.Equal(t, "pass", item.Status)
	require.Equal(t, http.StatusOK, item.HTTPStatus)
	require.Equal(t, "Tokyo, Japan", item.Message)
	require.Equal(t, "/ip/2a13:9500:124:0:9b3:e7f:cc93:82a5", requestedPath)
}

func TestRunProxyQualityIPFMLocation_NullLocationPasses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(ipfmSampleNullLocationHTML))
	}))
	defer server.Close()
	withIPFMBaseURL(t, server.URL+"/ip/")

	item := runProxyQualityIPFMLocation(context.Background(), server.Client(), "1.1.1.1")
	require.Equal(t, "pass", item.Status)
	require.Contains(t, item.Message, "未收录")
}

func TestRunProxyQualityIPFMLocation_WarnOnFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	withIPFMBaseURL(t, server.URL+"/ip/")

	item := runProxyQualityIPFMLocation(context.Background(), server.Client(), "8.8.8.8")
	require.Equal(t, "warn", item.Status)
	require.Equal(t, http.StatusBadGateway, item.HTTPStatus)

	item = runProxyQualityIPFMLocation(context.Background(), server.Client(), "")
	require.Equal(t, "warn", item.Status)
	require.Zero(t, item.HTTPStatus)
}
