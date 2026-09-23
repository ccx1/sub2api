package service

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/publicsuffix"
)

// 每轮使用独立 jar；实验模式只发布经过作用域检查的有界快照。
func newOpenAICodexTicketCookieJar() http.CookieJar {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return &codexTicketCookieJar{CookieJar: jar, entries: make(map[string]codexTicketCookieEntry)}
}

type codexTicketCookieEntry struct {
	cookie http.Cookie
	at     time.Time
	order  uint64
}

type codexTicketCookieJar struct {
	http.CookieJar
	mu       sync.Mutex
	entries  map[string]codexTicketCookieEntry
	revision uint64
	sequence uint64
}

func (j *codexTicketCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	if j == nil || u == nil || !strings.EqualFold(u.Hostname(), "chatgpt.com") || (u.Scheme != "https" && u.Scheme != "http") {
		return
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	now, changed := time.Now(), false
	for _, value := range cookies {
		if value == nil {
			continue
		}
		cookie, ok := normalizeCodexTicketCookie(*value, u, now)
		if !ok {
			continue
		}
		j.CookieJar.SetCookies(u, []*http.Cookie{&cookie})
		key := codexTicketCookieKey(cookie)
		if cookie.MaxAge < 0 || !cookie.Expires.IsZero() && !cookie.Expires.After(now) {
			_, exists := j.entries[key]
			changed = changed || exists
			delete(j.entries, key)
			continue
		}
		order := j.entries[key].order
		if order == 0 {
			j.sequence++
			order = j.sequence
		}
		j.entries[key] = codexTicketCookieEntry{cookie: cookie, at: now, order: order}
		changed = true
	}
	// 同值续期也产生候选，不能依赖两次 time.Now 的精度判断是否刷新。
	if changed {
		j.revision++
	}
}

func normalizeCodexTicketCookie(cookie http.Cookie, u *url.URL, now time.Time) (http.Cookie, bool) {
	if cookie.Valid() != nil {
		return cookie, false
	}
	cookie.Domain = strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
	if cookie.Domain != "" && cookie.Domain != "chatgpt.com" {
		return cookie, false
	}
	if !strings.HasPrefix(cookie.Path, "/") {
		cookie.Path = "/"
		if index := strings.LastIndex(u.Path, "/"); index > 0 {
			cookie.Path = u.Path[:index]
		}
	}
	if cookie.MaxAge > 0 {
		// 先限制秒数再转换 duration，避免恶意 Max-Age 溢出为过去时间。
		seconds := min(int64(cookie.MaxAge), int64((1<<63-1)/time.Second))
		cookie.Expires, cookie.MaxAge = now.Add(time.Duration(seconds)*time.Second), 0
	}
	cookie.Raw, cookie.RawExpires, cookie.Unparsed = "", "", nil
	return cookie, true
}

func codexTicketCookieKey(cookie http.Cookie) string {
	domain := strings.TrimPrefix(strings.ToLower(cookie.Domain), ".")
	if domain == "" {
		domain = "chatgpt.com"
	}
	return domain + "\x00" + cookie.Path + "\x00" + cookie.Name
}

func cloneCodexTicketCookieEntries(entries map[string]codexTicketCookieEntry) map[string]codexTicketCookieEntry {
	copy := make(map[string]codexTicketCookieEntry, len(entries))
	for key, entry := range entries {
		copy[key] = entry
	}
	return copy
}

// 版本只属于私有 jar；原请求的凭据快照保持不可变。
func codexTicketCookieJarRevision(jar http.CookieJar) uint64 {
	if typed, ok := jar.(*codexTicketCookieJar); ok && typed != nil {
		typed.mu.Lock()
		defer typed.mu.Unlock()
		return typed.revision
	}
	return 0
}

// 克隆保留收到时间、host-only 作用域和发送顺序，不能重置 session Cookie 的兜底期限。
func cloneOpenAICodexTicketCookieJar(jar http.CookieJar) http.CookieJar {
	source, ok := jar.(*codexTicketCookieJar)
	if !ok || source == nil {
		return nil
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	target := newOpenAICodexTicketCookieJar().(*codexTicketCookieJar)
	u, _ := url.Parse(chatgptCodexURL)
	entries := make([]codexTicketCookieEntry, 0, len(source.entries))
	for _, entry := range source.entries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, k int) bool { return entries[i].order < entries[k].order })
	for _, entry := range entries {
		cookie := entry.cookie
		target.CookieJar.SetCookies(u, []*http.Cookie{&cookie})
	}
	target.entries = cloneCodexTicketCookieEntries(source.entries)
	target.revision, target.sequence = source.revision, source.sequence
	return target
}

func newOpenAICodexTicketCookieJarFromSnapshot(cookies []*http.Cookie, captured time.Time, sessionKeys []string) http.CookieJar {
	jar := newOpenAICodexTicketCookieJar().(*codexTicketCookieJar)
	u, _ := url.Parse(chatgptCodexURL)
	sessions := make(map[string]bool, len(sessionKeys))
	for _, key := range sessionKeys {
		sessions[key] = true
	}
	for _, value := range cookies {
		if value == nil {
			continue
		}
		cookie := *value
		if sessions[codexTicketCookieKey(cookie)] {
			cookie.Expires, cookie.MaxAge = time.Time{}, 0
		}
		jar.SetCookies(u, []*http.Cookie{&cookie})
	}
	for key, entry := range jar.entries {
		// 缺失采集时间不能变成“刚收到”，否则重启会延长旧 session 凭据。
		entry.at = captured
		jar.entries[key] = entry
	}
	return jar
}

type openAICodexTicketCookieCandidate struct {
	Jar           http.CookieJar
	Revision      uint64
	HeaderChanged bool
	Cookies       []*http.Cookie
	SessionKeys   []string
	CapturedAt    time.Time
	ExpiresAt     time.Time
	HardExpiresAt time.Time
}

// candidateOpenAICodexTicketCookies applies a response's Set-Cookie headers
// to a cloned jar and returns the unverified candidate only when the complete
// jar actually changed. The caller must perform business verification before
// publishing the candidate.
func candidateOpenAICodexTicketCookies(jar http.CookieJar, req *http.Request, resp *http.Response, ttl time.Duration) (*openAICodexTicketCookieCandidate, bool) {
	if req == nil || req.URL == nil || resp == nil || len(resp.Cookies()) == 0 {
		return nil, false
	}
	base := jar
	if base == nil {
		base = newOpenAICodexTicketCookieJar()
	}
	clone := cloneOpenAICodexTicketCookieJar(base)
	if clone == nil {
		return nil, false
	}
	before := codexTicketCookieJarRevision(clone)
	u, _ := url.Parse(chatgptCodexURL)
	beforeHeader := codexTicketCookieHeaderValues(clone.Cookies(u))
	clone.SetCookies(req.URL, resp.Cookies())
	after := codexTicketCookieJarRevision(clone)
	if before == after {
		return nil, false
	}
	cookies, captured, expires := snapshotCodexTicketCookieLifetime(clone, ttl)
	return &openAICodexTicketCookieCandidate{Jar: clone, Revision: after, Cookies: cookies,
		HeaderChanged: beforeHeader != codexTicketCookieHeaderValues(clone.Cookies(u)),
		SessionKeys:   snapshotCodexTicketCookieSessionKeys(clone), HardExpiresAt: snapshotCodexTicketCookieHardExpiresAt(clone),
		CapturedAt: captured, ExpiresAt: expires}, true
}

func codexTicketCookieHeaderValues(cookies []*http.Cookie) string {
	values := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		values = append(values, cookie.Name+"="+cookie.Value)
	}
	return strings.Join(values, "; ")
}

func snapshotCodexTicketCookieSessionKeys(jar http.CookieJar) []string {
	keys, _ := codexTicketCookieExpiryMetadata(jar)
	return keys
}

func snapshotCodexTicketCookieHardExpiresAt(jar http.CookieJar) time.Time {
	_, expires := codexTicketCookieExpiryMetadata(jar)
	return expires
}

func codexTicketCookieExpiryMetadata(jar http.CookieJar) ([]string, time.Time) {
	j, ok := jar.(*codexTicketCookieJar)
	if !ok || j == nil {
		return nil, time.Time{}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	u, _ := url.Parse(chatgptCodexURL)
	var keys []string
	var hard time.Time
	for key, entry := range j.entries {
		if !codexTicketCookiePathMatches(entry.cookie.Path, u.Path) {
			continue
		}
		expires := entry.cookie.Expires
		if expires.IsZero() {
			keys = append(keys, key)
		} else if hard.IsZero() || expires.Before(hard) {
			hard = expires
		}
	}
	sort.Strings(keys)
	return keys, hard
}

func snapshotOpenAICodexTicketCookies(jar http.CookieJar, ttl time.Duration) ([]*http.Cookie, time.Time) {
	cookies, _, expires := snapshotCodexTicketCookieLifetime(jar, ttl)
	return cookies, expires
}

func snapshotCodexTicketCookieLifetime(jar http.CookieJar, ttl time.Duration) ([]*http.Cookie, time.Time, time.Time) {
	j, ok := jar.(*codexTicketCookieJar)
	if !ok || j == nil {
		return nil, time.Time{}, time.Time{}
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	u, _ := url.Parse(chatgptCodexURL)
	active, now := j.CookieJar.Cookies(u), time.Now()
	// 不能截断后发布未经验证的 Cookie 子集；超限时整轮不发布。
	if len(active) > 32 {
		return nil, time.Time{}, time.Time{}
	}
	for _, cookie := range active {
		if len(cookie.Name)+len(cookie.Value) > 4096 {
			return nil, time.Time{}, time.Time{}
		}
	}
	var result []*http.Cookie
	var expires time.Time
	var captured time.Time
	keys := make([]string, 0, len(j.entries))
	for key := range j.entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		entry := j.entries[key]
		cookie := entry.cookie
		deadline := cookie.Expires
		// Persistent cookies keep the upstream deadline. For session cookies there
		// is no server supplied hard expiry, so the configured Cookie TTL is the
		// compatibility fallback until a later response supplies one.
		if deadline.IsZero() {
			deadline = entry.at.Add(ttl)
		}
		if !deadline.After(now) || !codexTicketCookiePathMatches(cookie.Path, u.Path) {
			continue
		}
		for _, current := range active {
			if current.Name != cookie.Name || current.Value != cookie.Value {
				continue
			}
			cookie.Expires = deadline
			result = append(result, &cookie)
			if captured.IsZero() || entry.at.Before(captured) {
				captured = entry.at
			}
			if expires.IsZero() || deadline.Before(expires) {
				expires = deadline
			}
			break
		}
	}
	return result, captured, expires
}

func codexTicketCookiePathMatches(cookiePath, path string) bool {
	return cookiePath == path || strings.HasPrefix(path, cookiePath) && (strings.HasSuffix(cookiePath, "/") || strings.HasPrefix(strings.TrimPrefix(path, cookiePath), "/"))
}

func applyOpenAICodexTicketCookies(jar http.CookieJar, req *http.Request) {
	if jar == nil || req == nil || req.URL == nil {
		return
	}
	for _, cookie := range jar.Cookies(req.URL) {
		req.AddCookie(cookie)
	}
}

func storeOpenAICodexTicketCookies(jar http.CookieJar, req *http.Request, resp *http.Response) {
	if jar == nil || req == nil || req.URL == nil || resp == nil {
		return
	}
	jar.SetCookies(req.URL, resp.Cookies())
}

// Cookie 仅进入私有票据缓存，不随管理员报文历史持久化。
func redactOpenAICodexTicketCookies(capture *codexTicketExchangeCapture) {
	if capture == nil || capture.exchange == nil {
		return
	}
	for _, message := range []*CodexTicketHTTPMessage{capture.exchange.Request, capture.exchange.Response} {
		if message == nil {
			continue
		}
		for name := range message.Headers {
			if strings.EqualFold(name, "Cookie") || strings.EqualFold(name, "Set-Cookie") {
				message.Headers[name] = []string{"[REDACTED]"}
			}
		}
	}
}
