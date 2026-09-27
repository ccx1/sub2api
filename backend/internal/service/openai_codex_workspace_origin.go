package service

import (
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// B1/B2：workspace_backend_origin 路由。
//
// B1 只采集账号声明的 origin / routing_override，存入 credentials，供画像与可选
// 路由使用，绝不改变请求去向。
//
// B2 在开关为 probe 时，把 origin 作为打票探测目标 host，并用一个内存熔断/半开
// 状态机守护：origin 连续探测失败达阈值即静默、回落默认 chatgpt.com；静默到期后
// 放一个半开探测尝试恢复。任何时候 origin 缺失或不可用都回落默认 host——默认
// host 是永远的安全兜底。
//
// 关键区分：只有传输层/HTTP 不可达才计入 origin 失败；业务层降智（safety
// buffering / model_mismatch）不算 origin 的错，换 host 治不了降智。

const (
	// credentials 中 B1 采集字段的键。
	credentialWorkspaceBackendOrigin = "workspace_backend_origin"
	credentialAccountRoutingOverride = "account_routing_override"
)

// WorkspaceBackendOrigin 返回账号声明的后端 origin（B1 采集值），可能为空。
func (a *Account) WorkspaceBackendOrigin() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.GetCredential(credentialWorkspaceBackendOrigin))
}

// AccountRoutingOverride 返回账号地理路由约束（NO_CONSTRAINT/us/us_cr），可能为空。
func (a *Account) AccountRoutingOverride() string {
	if a == nil {
		return ""
	}
	return strings.TrimSpace(a.GetCredential(credentialAccountRoutingOverride))
}

// codexWorkspaceOriginHealth 是单账号 origin 的内存熔断状态。
type codexWorkspaceOriginHealth struct {
	origin           string
	consecutiveFail  int
	silencedUntil    time.Time
	halfOpenInFlight bool
}

var codexWorkspaceOriginHealthMu sync.Mutex
var codexWorkspaceOriginHealthByAccount = map[int64]*codexWorkspaceOriginHealth{}

func codexWorkspaceOriginHealthFor(accountID int64, origin string) *codexWorkspaceOriginHealth {
	h := codexWorkspaceOriginHealthByAccount[accountID]
	if h == nil || h.origin != origin {
		// origin 变化视为全新目标，重置健康状态。
		h = &codexWorkspaceOriginHealth{origin: origin}
		codexWorkspaceOriginHealthByAccount[accountID] = h
	}
	return h
}

// codexWorkspaceOriginResponsesURL 决定本次打票探测的目标 URL 与是否为半开探测。
// 返回 (url, host, isHalfOpenProbe, usingOrigin)。usingOrigin=false 表示回落默认。
func codexWorkspaceOriginResponsesURL(account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) (string, string, bool, bool) {
	// 开关关闭、cookie 凭据（cookie 绑定 chatgpt.com 域）或缺 origin：一律默认 host。
	if cfg.WorkspaceOriginRouting != config.CodexTicketWorkspaceOriginRoutingProbe ||
		config.CodexTicketUsesCookies(cfg) || account == nil {
		return chatgptCodexURL, "chatgpt.com", false, false
	}
	origin := account.WorkspaceBackendOrigin()
	u := codexWorkspaceOriginURL(origin)
	if u == "" {
		return chatgptCodexURL, "chatgpt.com", false, false
	}
	host := codexWorkspaceOriginHost(origin)
	if host == "" {
		return chatgptCodexURL, "chatgpt.com", false, false
	}

	codexWorkspaceOriginHealthMu.Lock()
	defer codexWorkspaceOriginHealthMu.Unlock()
	h := codexWorkspaceOriginHealthFor(account.ID, origin)
	if h.silencedUntil.After(now) {
		// 静默期内一律回落默认 host。
		return chatgptCodexURL, "chatgpt.com", false, false
	}
	// 静默到期（或从未静默）：使用 origin。若此前有失败累积，本次即半开探测。
	halfOpen := h.consecutiveFail > 0
	h.halfOpenInFlight = halfOpen
	return u, host, halfOpen, true
}

// codexWorkspaceOriginReportTransportFailure 记录 origin 传输层失败（不可达）。
func codexWorkspaceOriginReportTransportFailure(account *Account, cfg config.OpenAICodexTicketConfig, now time.Time) {
	if account == nil {
		return
	}
	origin := account.WorkspaceBackendOrigin()
	if origin == "" {
		return
	}
	threshold := cfg.WorkspaceOriginFailureThreshold
	if threshold <= 0 {
		threshold = 3
	}
	silence := time.Duration(cfg.WorkspaceOriginSilenceSeconds) * time.Second
	if silence <= 0 {
		silence = 600 * time.Second
	}
	codexWorkspaceOriginHealthMu.Lock()
	defer codexWorkspaceOriginHealthMu.Unlock()
	h := codexWorkspaceOriginHealthFor(account.ID, origin)
	h.halfOpenInFlight = false
	h.consecutiveFail++
	if h.consecutiveFail >= threshold {
		// 指数退避：静默时长随连续失败轮次翻倍，封顶 1 小时。
		mult := h.consecutiveFail - threshold
		backoff := silence
		for i := 0; i < mult && backoff < time.Hour; i++ {
			backoff *= 2
		}
		if backoff > time.Hour {
			backoff = time.Hour
		}
		h.silencedUntil = now.Add(backoff)
	}
}

// codexWorkspaceOriginReportSuccess 记录 origin 成功（可达且拿到合格响应），清熔断。
func codexWorkspaceOriginReportSuccess(account *Account) {
	if account == nil {
		return
	}
	origin := account.WorkspaceBackendOrigin()
	if origin == "" {
		return
	}
	codexWorkspaceOriginHealthMu.Lock()
	defer codexWorkspaceOriginHealthMu.Unlock()
	h := codexWorkspaceOriginHealthFor(account.ID, origin)
	h.consecutiveFail = 0
	h.halfOpenInFlight = false
	h.silencedUntil = time.Time{}
}

// codexWorkspaceOriginTransportUnreachable 判定本次 origin 探测是否属于
// 传输层/网关级不可达：真正的传输错误（超时/连接拒绝/DNS/TLS）或缺响应，
// 以及网关级 HTTP 状态（502/503/504 与 Cloudflare 520-524）。应用层状态
// （200/401/403/429 等）表示 host 可达，不计入 origin 熔断。
func codexWorkspaceOriginTransportUnreachable(resp *http.Response, err error) bool {
	if err != nil && resp == nil {
		return true
	}
	if resp == nil {
		return true
	}
	switch resp.StatusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout,
		520, 521, 522, 523, 524:
		return true
	}
	return false
}

func codexWorkspaceOriginURL(origin string) string {
	host := codexWorkspaceOriginHost(origin)
	if host == "" {
		return ""
	}
	return "https://" + host + "/backend-api/codex/responses"
}

// codexWorkspaceOriginHost 归一化 origin 为纯 host。接受 "https://x"、"x" 等形式。
// 只接受 openai.com / chatgpt.com 子域，避免把请求发到任意 host。
func codexWorkspaceOriginHost(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return ""
	}
	if !strings.Contains(origin, "://") {
		origin = "https://" + origin
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return ""
	}
	host := u.Hostname()
	if host == "" {
		return ""
	}
	lower := strings.ToLower(host)
	if lower == "chatgpt.com" {
		// 与默认等价，无需改路由。
		return ""
	}
	if strings.HasSuffix(lower, ".openai.com") || strings.HasSuffix(lower, ".chatgpt.com") {
		return host
	}
	return ""
}
