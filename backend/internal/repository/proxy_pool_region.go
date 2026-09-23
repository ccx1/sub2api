package repository

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func proxyMatchesRegion(proxy *service.Proxy, info *service.ProxyLatencyInfo, country string) bool {
	if country == "" {
		return true
	}
	if proxy == nil || !proxy.IsActive() || proxy.IsExpired(time.Now()) {
		return false
	}
	if info == nil {
		return strings.EqualFold(strings.TrimSpace(proxy.CountryCode), country)
	}
	if !service.ProxyLatencyMatchesProxy(info, proxy) {
		return false
	}
	probeCountry := strings.ToUpper(strings.TrimSpace(info.CountryCode))
	if info.Success && probeCountry != "" {
		return strings.EqualFold(probeCountry, country)
	}
	// A probe without a country (or a missing probe) may use the manually
	// configured value. A successful probe with another country is authoritative
	// and must never be overridden by stale/manual metadata.
	return strings.EqualFold(strings.TrimSpace(proxy.CountryCode), country)
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
