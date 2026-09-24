package service

import (
	"context"
	"net/http"
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
		if status.RouteAffinityConnections > 0 {
			status.RouteAffinityStatus = "available"
		}
	}
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
