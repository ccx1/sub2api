package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"net/http"
	"strings"
)

func normalizeOpenAIWSTransportCompatibility(req openAIWSAcquireRequest) openAIWSHandshakeCompatibilityKey {
	key := normalizeOpenAIWSHandshakeCompatibility(req.Account, req.Headers)
	if proxy := strings.TrimSpace(req.ProxyURL); proxy != "" {
		key.proxyIdentity = sha256.Sum256([]byte(proxy))
	}
	if profile, err := resolveMode1TLSProfile(req.Account); err == nil && profile != nil {
		key.tlsProfile = profile.CacheKey()
	}
	return key
}

func (p *openAIWSConnPool) dialWithAccountTransport(ctx context.Context, req openAIWSAcquireRequest, headers http.Header) (openAIWSClientConn, int, http.Header, error) {
	if p.cfg == nil || !p.cfg.Gateway.TLSFingerprint.Enabled {
		return p.clientDialer.Dial(ctx, req.WSURL, headers, req.ProxyURL)
	}
	profile, err := resolveMode1TLSProfile(req.Account)
	if err != nil {
		return nil, 0, nil, err
	}
	if profile == nil {
		return p.clientDialer.Dial(ctx, req.WSURL, headers, req.ProxyURL)
	}
	dialer, ok := p.clientDialer.(openAIWSClientTLSDialer)
	if !ok {
		return nil, 0, nil, errors.New("websocket dialer does not support the configured TLS profile")
	}
	return dialer.DialWithTLS(ctx, req.WSURL, headers, req.ProxyURL, profile)
}
