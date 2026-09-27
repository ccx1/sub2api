package repository

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func proxyMatchesRegion(proxy *service.Proxy, info *service.ProxyLatencyInfo, country string) bool {
	if country == "" {
		return true
	}
	current, ok := proxyEgressCountry(proxy, info)
	return ok && current != "" && current == service.CanonicalProxyCountry(country)
}

// proxyEgressCountry returns the country used for region matching and whether
// the proxy is eligible at all.
func proxyEgressCountry(proxy *service.Proxy, info *service.ProxyLatencyInfo) (string, bool) {
	if proxy == nil || !proxy.IsActive() || proxy.IsExpired(time.Now()) {
		return "", false
	}
	if info == nil {
		return service.CanonicalProxyCountry(proxy.CountryCode), true
	}
	if !service.ProxyLatencyMatchesProxy(info, proxy) {
		return "", false
	}
	probeCountry := service.CanonicalProxyCountry(info.CountryCode)
	if info.Success && probeCountry != "" {
		return probeCountry, true
	}
	// A probe without a country (or a missing probe) may use the manually
	// configured value. A successful probe with another country is authoritative
	// and must never be overridden by stale/manual metadata.
	return service.CanonicalProxyCountry(proxy.CountryCode), true
}

// proxyMatchesMacroRegion reports whether the proxy egress is in the same
// Codex macro-region as country. Unknown macro-regions never match.
func proxyMatchesMacroRegion(proxy *service.Proxy, info *service.ProxyLatencyInfo, country string) bool {
	want := service.CodexMacroRegionForCountry(country)
	if want == "" {
		return false
	}
	current, ok := proxyEgressCountry(proxy, info)
	return ok && service.CodexMacroRegionForCountry(current) == want
}

func filterProxyPoolRegion(candidates []proxyPoolCandidate, health map[int64]*service.ProxyLatencyInfo, country string) []proxyPoolCandidate {
	if country == "" {
		return candidates
	}
	filtered := make([]proxyPoolCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.proxy != nil && proxyMatchesRegion(candidate.proxy, health[candidate.proxy.ID], country) {
			filtered = append(filtered, candidate)
		}
	}
	return filtered
}

// 地区选路层级：账号地区 → 默认代理地区 → （允许回退时）同大区 → 整个代理池。
const (
	proxyPoolTierRegion         = "region"
	proxyPoolTierDefaultCountry = "default_country"
	proxyPoolTierMacroRegion    = "macro_region"
	proxyPoolTierPool           = "pool"
)

// selectProxyPoolRegion 逐级放宽地区条件，返回第一个含可用代理的层级。
// 是否放宽取决于筛选后是否还有可用代理（usable），而不是该地区是否配置过代理：
// 地区代理全部停用、过期或探测失败时同样进入下一层。账号地区与默认代理地区之后的
// 同大区、整池两层受 allowFallback 控制。所有层级都没有可用代理时返回账号地区层，
// 由调用方按空池策略处理。
func selectProxyPoolRegion(candidates []proxyPoolCandidate, health map[int64]*service.ProxyLatencyInfo, selection service.ProxyPoolSelection,
	usable func(*service.Proxy) bool) ([]proxyPoolCandidate, string) {
	country := service.CanonicalProxyCountry(selection.CountryCode)
	if country == "" {
		return candidates, proxyPoolTierRegion
	}
	regional := filterProxyPoolRegion(candidates, health, country)
	if hasUsableProxyPoolCandidate(regional, usable) {
		return regional, proxyPoolTierRegion
	}
	if fallback := service.CanonicalProxyCountry(selection.FallbackCountryCode); fallback != "" && fallback != country {
		if preferred := filterProxyPoolRegion(candidates, health, fallback); hasUsableProxyPoolCandidate(preferred, usable) {
			return preferred, proxyPoolTierDefaultCountry
		}
	}
	if !selection.AllowCountryFallback {
		return regional, proxyPoolTierRegion
	}
	macro := make([]proxyPoolCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.proxy != nil && proxyMatchesMacroRegion(candidate.proxy, health[candidate.proxy.ID], country) {
			macro = append(macro, candidate)
		}
	}
	if hasUsableProxyPoolCandidate(macro, usable) {
		return macro, proxyPoolTierMacroRegion
	}
	if hasUsableProxyPoolCandidate(candidates, usable) {
		return candidates, proxyPoolTierPool
	}
	return regional, proxyPoolTierRegion
}

func hasUsableProxyPoolCandidate(candidates []proxyPoolCandidate, usable func(*service.Proxy) bool) bool {
	for _, candidate := range candidates {
		if candidate.proxy != nil && (usable == nil || usable(candidate.proxy)) {
			return true
		}
	}
	return false
}

// proxyPoolUsable 与分配时的候选筛选保持一致：状态、有效期、健康探测和使用前质量检测均通过才算可用。
func proxyPoolUsable(health map[int64]*service.ProxyLatencyInfo, gate func(*service.Proxy, *service.ProxyLatencyInfo, bool, bool) (bool, bool)) func(*service.Proxy) bool {
	return func(proxy *service.Proxy) bool {
		_, degraded, valid := proxyPoolQuality(proxy, health[proxy.ID])
		if gate != nil {
			_, valid = gate(proxy, health[proxy.ID], degraded, valid)
		}
		return valid
	}
}

const proxyPoolRegionFallbackLogInterval = 10 * time.Minute

// proxyPoolRegionFallbackLogged 按账号与层级节流回退日志，避免亲和复用时每个请求都刷屏。
var proxyPoolRegionFallbackLogged sync.Map

func logProxyPoolRegionFallback(accountID int64, selection service.ProxyPoolSelection, tier string, proxyID int64) {
	if tier == proxyPoolTierRegion {
		return
	}
	key := strconv.FormatInt(accountID, 10) + ":" + selection.CountryCode + ":" + tier
	now := time.Now()
	if last, ok := proxyPoolRegionFallbackLogged.Load(key); ok && now.Sub(last.(time.Time)) < proxyPoolRegionFallbackLogInterval {
		return
	}
	proxyPoolRegionFallbackLogged.Store(key, now)
	attrs := []any{"account_id", accountID, "required_country", selection.CountryCode,
		"default_country", selection.FallbackCountryCode, "tier", tier, "proxy_id", proxyID}
	if tier == proxyPoolTierDefaultCountry {
		slog.Info("proxy_pool.region_fallback", attrs...)
		return
	}
	slog.Warn("proxy_pool.region_fallback: no active proxy in required country, falling back to wider pool", attrs...)
}

func (r *accountRepository) MatchesProxyRegion(ctx context.Context, proxy *service.Proxy, country string) (bool, error) {
	if country == "" {
		return true, nil
	}
	if proxy == nil || proxy.ID <= 0 {
		return false, nil
	}
	if r == nil || r.proxyPool == nil || r.proxyPool.latencyCache == nil {
		return false, errors.New("proxy region probe cache is unavailable")
	}
	health, err := r.proxyPool.latencyCache.GetProxyLatencies(ctx, []int64{proxy.ID})
	if err != nil {
		return false, err
	}
	return proxyMatchesRegion(proxy, health[proxy.ID], country), nil
}

// CodexProxyEgressCountry returns the egress country used for Codex node
// macro-region checks. An unknown country is returned as an empty string.
func (r *accountRepository) CodexProxyEgressCountry(ctx context.Context, proxyID int64) (string, error) {
	if proxyID <= 0 {
		return "", nil
	}
	if r == nil || r.proxyPool == nil || r.proxyPool.latencyCache == nil {
		return "", errors.New("proxy region probe cache is unavailable")
	}
	proxy, err := r.GetCodexTicketProxy(ctx, proxyID)
	if err != nil {
		return "", err
	}
	health, err := r.proxyPool.latencyCache.GetProxyLatencies(ctx, []int64{proxyID})
	if err != nil {
		return "", err
	}
	country, ok := proxyEgressCountry(proxy, health[proxyID])
	if !ok {
		return "", nil
	}
	return country, nil
}
