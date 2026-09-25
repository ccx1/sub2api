package service

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 管理列表只读取本进程连接池，不建立连接或触发质量探测。
func (s *OpenAIGatewayService) EnrichCodexRouteAffinityStatus(ctx context.Context, account *Account, statuses []OpenAICodexTicketStatus) {
	policy, enabled := s.codexRequestStrategyPolicyForScope(ctx, CodexRequestStrategyScopeDedicated)
	if account == nil {
		return
	}
	var counts map[string]int
	if enabled && policy.RouteAffinityMode != CodexRouteAffinityOff {
		counts = s.openAIWSRouteConnectionCounts(account.ID, policy.RouteAffinityMode)
	}
	for i := range statuses {
		status := &statuses[i]
		status.RouteAffinityStatus = CodexRouteAffinityOff
		status.RouteAffinityConnections = 0
		if !enabled || policy.RouteAffinityMode == CodexRouteAffinityOff {
			continue
		}
		status.RouteAffinityStatus = "unknown"
		if openAIModelRouteCooling(account, status.Model) {
			status.RouteAffinityStatus = "unavailable"
			continue
		}
		status.RouteAffinityConnections = counts[status.Model]
		// __oailb 能解析出 exp 时展示路由租约到期时间；解析不了则不展示。
		route, routeExpires := codexTicketRouteAffinityFromInventory(account, status.Model)
		if !routeExpires.IsZero() {
			status.RouteExpiresAt = &routeExpires
		}
		if status.RouteAffinityConnections > 0 {
			status.RouteAffinityStatus = "available"
			continue
		}
		// 本进程暂无同路由连接时，回退到票据已捕获的路由标记（__oailb/__cflb）。
		// 有路由标记即视为“已探测/路由已知”，避免长期停留在“待探测”；没有标记则
		// 说明当前 Cookie 模式未保留路由字段，据此提示而不是一直显示未知。
		switch route {
		case "route_known":
			status.RouteAffinityStatus = "available"
		case "no_route_cookie":
			status.RouteAffinityStatus = "unavailable"
		}
	}
}

// codexTicketRouteAffinityFromInventory 从账号库存票据推导路由标记状态：
// route_known 表示票据携带有效的 __oailb/__cflb 路由指纹；no_route_cookie 表示
// 是 Cookie 票但不含路由字段（例如被 strip_routing 去掉）；空字符串表示无从判断。
// 第二个返回值是路由已知时最晚一张票可解析出的 __oailb exp，解析不了为零值。
func codexTicketRouteAffinityFromInventory(account *Account, model string) (string, time.Time) {
	if account == nil || account.Extra == nil {
		return "", time.Time{}
	}
	inventory := parseOpenAICodexTicketFromAny(account.ID, model, account.Extra[openAICodexTicketExtraKey(model)])
	now := time.Now()
	result := ""
	var routeExpires time.Time
	for _, slot := range codexTicketSlots(inventory) {
		if slot == nil || slot.Revoked || !slot.usesCookies() {
			continue
		}
		if !slot.hardExpiresAt().IsZero() && !slot.hardExpiresAt().After(now) {
			continue
		}
		if codexTicketRouteFingerprint(slot) != "" {
			result = "route_known"
			if expires := codexTicketRouteExpiresAt(slot); expires.After(now) && expires.After(routeExpires) {
				routeExpires = expires
			}
		}
	}
	if result != "" {
		return result, routeExpires
	}
	if inventory != nil && inventory.usesCookies() {
		return "no_route_cookie", time.Time{}
	}
	return "", time.Time{}
}

// codexTicketRouteFingerprint 用票据实际会发送的 Cookie 计算路由指纹。
func codexTicketRouteFingerprint(ticket *openAICodexTicket) string {
	if ticket == nil {
		return ""
	}
	headers := make(http.Header)
	u, _ := url.Parse(chatgptCodexURL)
	req := &http.Request{Header: headers}
	for _, cookie := range ticket.cookiesForURL(u) {
		if cookie != nil && openAIWSRouteCookie(cookie.Name) {
			req.AddCookie(cookie)
		}
	}
	return openAIWSRequestRouteFingerprint(req.Header)
}

func openAIModelRouteCooling(account *Account, model string) bool {
	limits, _ := account.Extra[modelRateLimitsKey].(map[string]any)
	state, _ := limits[model].(map[string]any)
	return state["reason"] == openAIWSRouteAffinityUnavailableReason && account.isRateLimitActiveForKey(model)
}

func (s *OpenAIGatewayService) openAIWSRouteConnectionCounts(accountID int64, mode string) map[string]int {
	counts := make(map[string]int)
	if s == nil {
		return counts
	}
	// 通过 Once 读取，避免与首个业务请求初始化池并发竞争。
	ap, ok := s.getOpenAIWSConnPool().getAccountPool(accountID)
	if !ok || ap == nil {
		return counts
	}
	ap.mu.Lock()
	defer ap.mu.Unlock()
	for _, conn := range ap.conns {
		if conn == nil || conn.isClosed() || conn.isUnusable() || conn.codexTicketReceipt == nil ||
			mode == CodexRouteAffinityStrict && conn.routeRequest == nil {
			continue
		}
		r := conn.codexTicketReceipt
		headers := make(http.Header)
		r.ticket.applyHeaders(headers)
		headers.Set(openAICodexRoutingHintHeader, strings.TrimSpace(conn.routingAffinity))
		req := openAIWSAcquireRequest{Account: r.account, Headers: headers, CodexTicketReceipt: r, RouteAffinityMode: CodexRouteAffinityStrict}
		if validateOpenAIWSRouteRequest(req, time.Now()) == nil && conn.matchesRouteFingerprint(openAIWSRequestRouteFingerprint(headers)) {
			counts[r.ticket.Model]++
		}
	}
	return counts
}
