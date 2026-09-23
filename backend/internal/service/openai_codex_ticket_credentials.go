package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func (s *OpenAIGatewayService) openAICodexTicketConfigForAccount(ctx context.Context, account *Account) config.OpenAICodexTicketConfig {
	return resolveCodexTicketCredentialConfig(account, s.openAICodexTicketConfigContext(ctx))
}

func (t *openAICodexTicket) usesCookies() bool {
	return t != nil && (t.CredentialMode == config.CodexTicketCredentialCookie || t.CredentialMode == config.CodexTicketCredentialCookieState)
}

func (t *openAICodexTicket) credentialIdentity() string {
	if t.SessionID != "" {
		encoded, _ := json.Marshal(struct {
			Mode, State, SessionID, Egress string
			Cookies                        []*http.Cookie
		}{t.CredentialMode, t.State, t.SessionID, t.Egress, t.Cookies})
		return "v2:" + openAICodexTicketEgress(string(encoded))
	}
	// 旧票没有 session，保留原身份编码和撤销记录；新票绑定完整凭据快照。
	if !t.usesCookies() {
		return t.State
	}
	encoded, _ := json.Marshal(struct {
		Mode, State string
		Cookies     []*http.Cookie
	}{t.CredentialMode, t.State, t.Cookies})
	return openAICodexTicketEgress(string(encoded))
}

func (t *openAICodexTicket) cookiesForURL(u *url.URL) []*http.Cookie {
	if !t.usesCookies() || u == nil || u.Hostname() != "chatgpt.com" || u.Scheme != "https" && u.Scheme != "wss" {
		return nil
	}
	if u.Path != "/backend-api/codex/responses" && u.Path != "/backend-api/codex/responses/compact" {
		return nil
	}
	var result []*http.Cookie
	for _, cookie := range t.Cookies {
		if cookie != nil && cookie.Valid() == nil && (cookie.Domain == "" || strings.TrimPrefix(cookie.Domain, ".") == "chatgpt.com") &&
			cookie.MaxAge >= 0 && cookie.Expires.After(time.Now()) && codexTicketCookiePathMatches(cookie.Path, u.Path) {
			copy := *cookie
			result = append(result, &copy)
		}
	}
	return result
}

func (t *openAICodexTicket) cookieUsable(now time.Time, cfg config.OpenAICodexTicketConfig) bool {
	if !t.usesCookies() || t.CredentialMode != cfg.CredentialMode || t.Revoked || !t.effectiveExpiresAt(cfg).After(now) ||
		len(t.Cookies) == 0 || len(t.Cookies) > 32 || !t.Verified && !t.VerificationSkipped {
		return false
	}
	if t.CredentialMode == config.CodexTicketCredentialCookieState && !codexTicketAutoStateShape(t.State) {
		return false
	}
	for _, cookie := range t.Cookies {
		if cookie == nil || cookie.Valid() != nil || !cookie.Expires.After(now) {
			return false
		}
	}
	u, _ := url.Parse(chatgptCodexURL)
	return len(t.cookiesForURL(u)) == len(t.Cookies)
}

func (t *openAICodexTicket) effectiveExpiresAt(cfg config.OpenAICodexTicketConfig) time.Time {
	// 配置 TTL 只决定何时进入后台复验；真实凭据仍在硬期限内时继续服务。
	// ExpiresAt 已在发布时取 STATE、Cookie 和协议安全期限的最小值。
	return t.hardExpiresAt()
}

func (t *openAICodexTicket) applyHeaders(h http.Header) {
	if strings.TrimSpace(t.SessionID) != "" {
		h.Set("session_id", t.SessionID)
	}
	if t.CredentialMode == config.CodexTicketCredentialCookie {
		h.Del(openAICodexTurnStateHeader)
	} else {
		h.Set(openAICodexTurnStateHeader, t.State)
	}
	if !t.usesCookies() {
		return
	}
	// 凭据来自同一已验证快照，不能混入客户端或上一版本的 Cookie。
	h.Del("Cookie")
	u, _ := url.Parse(chatgptCodexURL)
	request := &http.Request{Header: h}
	for _, cookie := range t.cookiesForURL(u) {
		request.AddCookie(cookie)
	}
}

func (t *openAICodexTicket) matchesHeaders(h http.Header) bool {
	if t == nil {
		return false
	}
	if t.SessionID != "" && h.Get("session_id") != t.SessionID {
		return false
	}
	if !t.usesCookies() {
		return h.Get(openAICodexTurnStateHeader) == t.State
	}
	if t.CredentialMode == config.CodexTicketCredentialCookie && h.Get(openAICodexTurnStateHeader) != "" {
		return false
	}
	if t.CredentialMode == config.CodexTicketCredentialCookieState && h.Get(openAICodexTurnStateHeader) != t.State {
		return false
	}
	request := &http.Request{Header: h}
	actual := request.Cookies()
	if len(actual) != len(t.Cookies) {
		return false
	}
	for _, expected := range t.Cookies {
		found := false
		for _, cookie := range actual {
			if expected != nil && cookie.Name == expected.Name && cookie.Value == expected.Value {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return len(actual) > 0
}

func (t *openAICodexTicket) clearHeaders(h http.Header) {
	if t == nil {
		return
	}
	if t.SessionID != "" && h.Get("session_id") == t.SessionID {
		h.Del("session_id")
	}
	if h.Get(openAICodexTurnStateHeader) == t.State {
		h.Del(openAICodexTurnStateHeader)
	}
	if !t.usesCookies() {
		return
	}
	request := &http.Request{Header: h}
	cookies := request.Cookies()
	h.Del("Cookie")
	for _, cookie := range cookies {
		managed := false
		for _, old := range t.Cookies {
			if old != nil && old.Name == cookie.Name && old.Value == cookie.Value {
				managed = true
				break
			}
		}
		if !managed {
			request.AddCookie(cookie)
		}
	}
}

// 识别未经候选处理的 Cookie 改变；未经模型复验不能发布新凭据。
func (t *openAICodexTicket) cookieResponseChanged(headers http.Header) bool {
	if !t.usesCookies() {
		return false
	}
	response := &http.Response{Header: headers}
	now := time.Now()
	for _, changed := range response.Cookies() {
		matched := false
		for _, old := range t.Cookies {
			if old == nil || changed.Name != old.Name {
				continue
			}
			matched = true
			if changed.Value != old.Value || changed.MaxAge < 0 ||
				changed.MaxAge > 0 && float64(changed.MaxAge) < old.Expires.Sub(now).Seconds() ||
				changed.MaxAge == 0 && !changed.Expires.IsZero() && changed.Expires.Before(old.Expires) {
				return true
			}
		}
		if !matched {
			// A newly introduced cookie is also a credential transition. The
			// candidate jar handles it without mutating the in-flight snapshot.
			return true
		}
	}
	return false
}

func codexTicketProbeBusiness(in openAICodexTicketProbeInput) bool {
	return in.BusinessVerification || in.State != ""
}

func probeCookieTTL(in openAICodexTicketProbeInput) time.Duration {
	if in.Config == nil {
		return 0
	}
	cfg := config.NormalizeCodexTicketCredentialConfig(*in.Config)
	if cfg.CookieTTLSeconds > 0 {
		return time.Duration(cfg.CookieTTLSeconds) * time.Second
	}
	return time.Duration(cfg.TTLSeconds) * time.Second
}

func codexTicketBusinessCookieSnapshot(in openAICodexTicketProbeInput) *openAICodexTicket {
	if !codexTicketProbeBusiness(in) || in.Config == nil || !config.CodexTicketUsesCookies(*in.Config) {
		return nil
	}
	cookies, captured, expires := snapshotCodexTicketCookieLifetime(in.CookieJar, probeCookieTTL(in))
	return &openAICodexTicket{CredentialMode: in.Config.CredentialMode, State: in.State, Cookies: cookies,
		CapturedAt: captured, ExpiresAt: expires, Verified: true}
}

func codexTicketBusinessCookieSnapshotForProbe(in openAICodexTicketProbeInput) *openAICodexTicket {
	if in.BusinessCredentialSnapshot != nil && in.BusinessCredentialSnapshot.usesCookies() {
		return in.BusinessCredentialSnapshot
	}
	return codexTicketBusinessCookieSnapshot(in)
}

func codexTicketProbeCandidateAccepted(in openAICodexTicketProbeInput, state string) bool {
	if in.Config == nil {
		return false
	}
	if !config.CodexTicketUsesCookies(*in.Config) {
		return codexTicketCandidateAccepted(state, in.Account, *in.Config)
	}
	cookies, _ := snapshotOpenAICodexTicketCookies(in.CookieJar, probeCookieTTL(in))
	return len(cookies) > 0 && (in.Config.CredentialMode == config.CodexTicketCredentialCookie || codexTicketAutoStateShape(state))
}
