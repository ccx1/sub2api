package openaiauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

// transport 复刻 customer_auth.py 的 Transport：固定 TLS 指纹、限定源、禁重定向、带 cookie。
//
// 与 Python 版一致的安全约束：只允许访问 auth.openai.com / sentinel.openai.com，
// 且所有请求都不自动跟随重定向（由 login 状态机手工推进）。
// 差异点：用本项目的 tlsfingerprint（utls）替代 curl_cffi，并支持 IP 池代理注入。

var allowedAuthHosts = map[string]bool{
	"auth.openai.com":     true,
	"sentinel.openai.com": true,
}

type transport struct {
	ctx       context.Context
	client    *http.Client
	userAgent string
	secCHUA   string
	deadline  time.Time
}

// newTransport 构造带 TLS 指纹与可选代理的传输层。proxyURL 为空表示直连。
func newTransport(proxyURL string) (*transport, error) {
	profile := tlsfingerprint.BuiltinProfile("nodejs24")
	if profile == nil {
		return nil, errors.New("tls profile unavailable")
	}
	// TLS 走 http/1.1，避免 h2 协商后行为不一致。
	profile = profile.Clone()
	profile.ALPNProtocols = []string{"http/1.1"}

	ua, secCHUA := randomChromeFingerprint()
	tr := &http.Transport{
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
		ForceAttemptHTTP2:   false,
	}
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		tr.DialTLSContext = tlsfingerprint.NewDialer(profile, nil).DialTLSContext
	} else {
		parsed, err := url.Parse(proxyURL)
		if err != nil || parsed.Hostname() == "" {
			return nil, fmt.Errorf("invalid proxy url: %q", proxyURL)
		}
		switch parsed.Scheme {
		case "http", "https":
			tr.DialTLSContext = tlsfingerprint.NewHTTPProxyDialer(profile, parsed).DialTLSContext
		case "socks5", "socks5h":
			tr.DialTLSContext = tlsfingerprint.NewSOCKS5ProxyDialer(profile, parsed).DialTLSContext
		default:
			return nil, fmt.Errorf("unsupported proxy scheme: %s", parsed.Scheme)
		}
	}
	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Transport: tr,
		Jar:       jar,
		// 禁止自动重定向：与 Python Transport(allow_redirects=False) 一致。
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       0, // 单请求超时在 do() 里按剩余时间控制
	}
	return &transport{
		client:    client,
		userAgent: ua,
		secCHUA:   secCHUA,
		deadline:  time.Now().Add(180 * time.Second),
	}, nil
}

// randomChromeFingerprint 复刻 customer_auth_security.random_chrome_fingerprint 的 UA/sec-ch-ua。
func randomChromeFingerprint() (ua, secCHUA string) {
	profiles := []struct {
		major, build int
		secCHUA      string
	}{
		{136, 7103, `"Google Chrome";v="136", "Chromium";v="136", "Not A(Brand";v="24"`},
		{131, 6778, `"Google Chrome";v="131", "Chromium";v="131", "Not A(Brand";v="24"`},
		{124, 6367, `"Google Chrome";v="124", "Chromium";v="124", "Not A(Brand";v="24"`},
	}
	p := profiles[randInt(len(profiles))]
	patch := randInt(200)
	ua = fmt.Sprintf("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/%d.0.%d.%d Safari/537.36", p.major, p.build, patch)
	return ua, p.secCHUA
}

// checkOrigin 强制只访问受信任的 auth / sentinel 源。
func checkOrigin(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return errUnexpectedOrigin
	}
	if u.Scheme != "https" || !allowedAuthHosts[u.Host] {
		return errUnexpectedOrigin
	}
	return nil
}

var (
	errUnexpectedOrigin = errors.New("unexpected_auth_origin")
	errDeadlineExceeded = errors.New("login_deadline_exceeded")
	errNetwork          = errors.New("auth_network_error")
)

// do 执行单个请求，限定源与剩余时间，返回 *http.Response（调用方负责关闭 Body）。
func (t *transport) do(req *http.Request) (*http.Response, error) {
	if err := checkOrigin(req.URL.String()); err != nil {
		return nil, err
	}
	left := time.Until(t.deadline)
	if left <= 0 {
		return nil, errDeadlineExceeded
	}
	timeout := 18 * time.Second
	if left < timeout {
		timeout = left
	}
	ctx, cancel := reqContext(req, timeout)
	req = req.WithContext(ctx)
	resp, err := t.client.Do(req)
	if err != nil {
		contextErr := ctx.Err()
		cancel()
		if contextErr != nil {
			return nil, contextErr
		}
		return nil, errNetwork
	}
	// 用包装体在 Body 关闭时释放 context。
	resp.Body = &cancelBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

// post 供 SentinelSolver 使用：发送 body，返回响应字节。sentinel 请求同样受源限制。
func (t *transport) post(rawURL string, body []byte, headers map[string]string) ([]byte, error) {
	req, err := t.newRequest(http.MethodPost, rawURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := t.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sentinel status %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// newRequest 构造带默认头的请求。
func (t *transport) newRequest(method, rawURL string, body io.Reader) (*http.Request, error) {
	ctx := t.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", t.userAgent)
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("sec-ch-ua", t.secCHUA)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	return req, nil
}

func (t *transport) close() {
	if t.client != nil {
		t.client.CloseIdleConnections()
	}
}

// newUsageTransport 构造用于 chatgpt.com usage 测活的客户端（带 TLS 指纹 + 可选代理）。
// 与 newTransport 不同，它不限制访问源，也允许自动重定向。
func newUsageTransport(proxyURL string) (*transport, error) {
	profile := tlsfingerprint.BuiltinProfile("nodejs24")
	if profile == nil {
		return nil, errors.New("tls profile unavailable")
	}
	profile = profile.Clone()
	profile.ALPNProtocols = []string{"http/1.1"}
	ua, secCHUA := randomChromeFingerprint()
	tr := &http.Transport{
		MaxIdleConns:        16,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
		TLSHandshakeTimeout: 15 * time.Second,
		ForceAttemptHTTP2:   false,
	}
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		tr.DialTLSContext = tlsfingerprint.NewDialer(profile, nil).DialTLSContext
	} else {
		parsed, err := url.Parse(proxyURL)
		if err != nil || parsed.Hostname() == "" {
			return nil, fmt.Errorf("invalid proxy url: %q", proxyURL)
		}
		switch parsed.Scheme {
		case "http", "https":
			tr.DialTLSContext = tlsfingerprint.NewHTTPProxyDialer(profile, parsed).DialTLSContext
		case "socks5", "socks5h":
			tr.DialTLSContext = tlsfingerprint.NewSOCKS5ProxyDialer(profile, parsed).DialTLSContext
		default:
			return nil, fmt.Errorf("unsupported proxy scheme: %s", parsed.Scheme)
		}
	}
	jar, _ := cookiejar.New(nil)
	return &transport{
		client:    &http.Client{Transport: tr, Jar: jar, Timeout: 35 * time.Second},
		userAgent: ua,
		secCHUA:   secCHUA,
		deadline:  time.Now().Add(60 * time.Second),
	}, nil
}
