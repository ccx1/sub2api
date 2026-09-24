package service

import (
	"errors"
	"net/http"
	"strings"
)

const (
	CodexCookiePreserve            = "preserve"
	CodexCookieStripRouting        = "strip_routing"
	CodexCookieStripCloudflare     = "strip_cloudflare"
	CodexCookieStripInfrastructure = "strip_infrastructure"
)

func normalizeCodexCookieMode(mode string) string {
	if mode == "" {
		return CodexCookiePreserve
	}
	return mode
}

func validateCodexCookieMode(mode string, strictAffinity bool) error {
	switch mode {
	case CodexCookiePreserve, CodexCookieStripRouting, CodexCookieStripCloudflare, CodexCookieStripInfrastructure:
	default:
		return errors.New("cookie_mode must be preserve, strip_routing, strip_cloudflare or strip_infrastructure")
	}
	if strictAffinity && codexCookieExcluded(mode, "__oailb") {
		return errors.New("strict route affinity is incompatible with cookie_mode that strips routing cookies")
	}
	return nil
}

func codexCookieExcluded(mode, name string) bool {
	// OpenAI infrastructure routing cookie, not an authentication cookie.
	routing := name == "__oailb" || name == "__cflb"
	switch mode {
	case CodexCookieStripRouting:
		return routing
	case CodexCookieStripCloudflare:
		return codexCloudflareStateCookie(name)
	case CodexCookieStripInfrastructure:
		return routing || codexCloudflareStateCookie(name)
	default:
		return false
	}
}

func codexCloudflareStateCookie(name string) bool {
	switch name {
	case "__cf_bm", "__cfruid", "__cfseq", "__cfwaitingroom", "_cfuvid",
		"cf_clearance", "cf_ob_info", "cf_use_ob":
		return true
	default:
		return strings.HasPrefix(name, "cf_chl_")
	}
}

func filterCodexCookieHeader(headers http.Header, mode string) {
	if normalizeCodexCookieMode(mode) == CodexCookiePreserve {
		return
	}
	for key, values := range headers {
		if !strings.EqualFold(key, "Cookie") {
			continue
		}
		filtered := make([]string, 0, len(values))
		changed := false
		for _, value := range values {
			next, removed := filterCodexCookieHeaderValue(value, mode)
			changed = changed || removed
			if next != "" || !removed {
				filtered = append(filtered, next)
			}
		}
		if !changed {
			continue
		}
		if len(filtered) == 0 {
			delete(headers, key)
		} else {
			headers[key] = filtered
		}
	}
}

func filterCodexCookieHeaderValue(value, mode string) (string, bool) {
	var kept []string
	changed := false
	for _, part := range strings.Split(value, ";") {
		name, _, _ := strings.Cut(strings.TrimSpace(part), "=")
		if codexCookieExcluded(mode, strings.TrimSpace(name)) {
			changed = true
			continue
		}
		if strings.TrimSpace(part) != "" {
			kept = append(kept, part)
		}
	}
	if !changed {
		return value, false
	}
	// 不经过 Cookie.String() 重编码，避免改变保留值中的引号或等号。
	return strings.TrimSpace(strings.Join(kept, ";")), true
}

func filterCodexCookies(cookies []*http.Cookie, mode string) []*http.Cookie {
	if normalizeCodexCookieMode(mode) == CodexCookiePreserve || cookies == nil {
		return cookies
	}
	filtered := make([]*http.Cookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie == nil || !codexCookieExcluded(mode, cookie.Name) {
			filtered = append(filtered, cookie)
		}
	}
	return filtered
}
