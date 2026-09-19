package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

type openAIWSClientTLSDialer interface {
	DialWithTLS(context.Context, string, http.Header, string, *tlsfingerprint.Profile) (openAIWSClientConn, int, http.Header, error)
}

func (d *coderOpenAIWSClientDialer) DialWithTLS(ctx context.Context, target string, headers http.Header, proxyURL string, profile *tlsfingerprint.Profile) (openAIWSClientConn, int, http.Header, error) {
	if profile == nil {
		return d.Dial(ctx, target, headers, proxyURL)
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Scheme != "wss" || parsed.Host == "" {
		return nil, 0, nil, errors.New("TLS fingerprint requires a valid wss target")
	}
	client, err := d.fingerprintHTTPClient(target, proxyURL, profile)
	if err != nil {
		return nil, 0, nil, err
	}
	return d.dialWithClient(ctx, target, headers, proxyURL, client)
}

func (d *coderOpenAIWSClientDialer) fingerprintHTTPClient(target, rawProxy string, profile *tlsfingerprint.Profile) (*http.Client, error) {
	if d == nil {
		return nil, errors.New("openai ws dialer is nil")
	}
	key := fmt.Sprintf("tls:%x", sha256.Sum256([]byte(target+"\x00"+rawProxy+"\x00"+profile.CacheKey())))
	now := time.Now().UnixNano()
	d.proxyMu.Lock()
	defer d.proxyMu.Unlock()
	if entry := d.proxyClients[key]; entry != nil {
		entry.lastUsedUnixNano = now
		d.proxyHits.Add(1)
		return entry.client, nil
	}
	transport, err := newOpenAIWSFingerprintTransport(rawProxy, profile)
	if err != nil {
		return nil, err
	}
	d.cleanupProxyClientsLocked(now)
	client := &http.Client{Transport: transport}
	if d.proxyClients == nil {
		d.proxyClients = make(map[string]*openAIWSProxyClientEntry)
	}
	d.proxyClients[key] = &openAIWSProxyClientEntry{client: client, lastUsedUnixNano: now}
	d.ensureProxyClientCapacityLocked()
	d.proxyMisses.Add(1)
	return client, nil
}

func newOpenAIWSFingerprintTransport(rawProxy string, profile *tlsfingerprint.Profile) (*http.Transport, error) {
	profile = profile.Clone()
	// WebSocket 升级使用 HTTP/1.1，禁止模板协商 h2 后发送 HTTP/1.1。
	profile.ALPNProtocols = []string{"http/1.1"}
	transport := &http.Transport{
		MaxIdleConns: openAIWSProxyTransportMaxIdleConns, MaxIdleConnsPerHost: openAIWSProxyTransportMaxIdleConnsPerHost,
		IdleConnTimeout: openAIWSProxyTransportIdleConnTimeout, TLSHandshakeTimeout: 10 * time.Second,
		DialTLSContext: tlsfingerprint.NewDialer(profile, nil).DialTLSContext,
	}
	if strings.TrimSpace(rawProxy) == "" {
		return transport, nil
	}
	parsed, err := url.Parse(rawProxy)
	if err != nil || parsed.Hostname() == "" {
		return nil, errors.New("invalid websocket proxy URL")
	}
	switch parsed.Scheme {
	case "http", "https":
		transport.DialTLSContext = tlsfingerprint.NewHTTPProxyDialer(profile, parsed).DialTLSContext
	case "socks5", "socks5h":
		transport.DialTLSContext = tlsfingerprint.NewSOCKS5ProxyDialer(profile, parsed).DialTLSContext
	default:
		return nil, errors.New("unsupported websocket proxy scheme")
	}
	return transport, nil
}
