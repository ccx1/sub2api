package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

// 路由标记只能证明 Cookie 亲和一致，不能证明物理节点身份或模型能力。
func openAIWSRouteCookie(name string) bool {
	return name == "__oailb" || name == "__cflb"
}

func openAIWSRouteFingerprint(headers http.Header) string {
	return openAIWSHandshakeRouteFingerprint(nil, headers)
}

func openAIWSRequestRouteFingerprint(headers http.Header) string {
	return openAIWSHandshakeRouteFingerprint(headers, nil)
}

func openAIWSHandshakeRouteFingerprint(request, response http.Header) string {
	markers := make(map[string]string)
	for _, cookie := range (&http.Request{Header: request}).Cookies() {
		if openAIWSRouteCookie(cookie.Name) && cookie.Value != "" {
			if value, exists := markers[cookie.Name]; exists && value != cookie.Value {
				return "" // 同名不同值无法确认实际上游采用哪条路由。
			}
			markers[cookie.Name] = cookie.Value
		}
	}
	for _, cookie := range (&http.Response{Header: response}).Cookies() {
		if !openAIWSRouteCookie(cookie.Name) {
			continue
		}
		if cookie.Value == "" || cookie.MaxAge < 0 || !cookie.Expires.IsZero() && !cookie.Expires.After(time.Now()) {
			delete(markers, cookie.Name)
		} else {
			markers[cookie.Name] = cookie.Value
		}
	}
	if len(markers) == 0 {
		return ""
	}
	values := make([]string, 0, len(markers))
	for name, value := range markers {
		values = append(values, name+"="+value)
	}
	sort.Strings(values)
	sum := sha256.Sum256([]byte(strings.Join(values, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func validateOpenAIWSRouteRequest(req openAIWSAcquireRequest, now time.Time) error {
	if normalizeOpenAIWSRouteAffinityMode(req.RouteAffinityMode) != CodexRouteAffinityStrict {
		return nil
	}
	r := req.CodexTicketReceipt
	model, _, _ := strings.Cut(strings.TrimPrefix(normalizeOpenAIWSRoutingAffinity(req.Headers), "model="), ";")
	if r == nil || !validOpenAICodexTicketWSReceiptAccount(req) || !r.ticket.usesCookies() ||
		!r.ticket.Verified || r.ticket.VerificationSkipped || !r.ticket.usable(now, req.Account, r.config) ||
		!r.ticket.matchesHeaders(req.Headers) || openAIWSRequestRouteFingerprint(req.Headers) == "" ||
		model == "" || model != r.ticket.Model || !codexTicketConfigGatesModel(r.config, model) || req.RouteQualityBlocked {
		return errOpenAIWSRouteAffinityUnavailable
	}
	if r.service != nil && r.service.codexTicketRevoked(openAICodexTicketKey(req.Account.ID, r.ticket.Model), &r.ticket) {
		return errOpenAIWSRouteAffinityUnavailable
	}
	return nil
}

func (c *openAIWSConn) validateRouteWrite(value any) error {
	if c.routeRequest == nil {
		return nil
	}
	if err := validateOpenAIWSRouteRequest(*c.routeRequest, time.Now()); err != nil {
		return err
	}
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	model := gjson.GetBytes(body, "model").String()
	if model != "" && model != c.routeRequest.CodexTicketReceipt.ticket.Model {
		return errOpenAIWSRouteAffinityUnavailable
	}
	return nil
}

func openAIWSConnMatchesRouteRequest(conn *openAIWSConn, req openAIWSAcquireRequest) bool {
	return conn != nil && conn.matchesHandshakeCompatibility(normalizeOpenAIWSTicketCompatibility(req)) &&
		conn.matchesRoutingAffinity(normalizeOpenAIWSRoutingAffinity(req.Headers)) &&
		conn.matchesRouteFingerprint(openAIWSRequestRouteFingerprint(req.Headers))
}

func pickUnusedOpenAIWSRouteConnLocked(ap *openAIWSAccountPool, req openAIWSAcquireRequest) *openAIWSConn {
	for _, conn := range ap.conns {
		if conn != nil && !conn.leasedBefore.Load() && !conn.isClosed() && !conn.isLeased() &&
			!conn.isUnusable() && openAIWSConnMatchesRouteRequest(conn, req) {
			return conn
		}
	}
	return nil
}

func countOpenAIWSRouteIdleLocked(ap *openAIWSAccountPool, req openAIWSAcquireRequest) int {
	count := 0
	for _, conn := range ap.conns {
		if conn == nil || conn.isClosed() || conn.isUnusable() || conn.isLeased() ||
			req.ForceNewConn && conn.leasedBefore.Load() || !openAIWSConnMatchesRouteRequest(conn, req) {
			continue
		}
		count++
	}
	return count
}
