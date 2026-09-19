package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	dbproxy "github.com/Wei-Shaw/sub2api/ent/proxy"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const proxyPoolLeaseTTL = 10 * time.Minute

type proxyPoolSettings interface {
	GetProxyPoolMaxAccounts(context.Context) (int, error)
}

type proxyPoolCandidate struct {
	proxy    *service.Proxy
	fixedIDs []string
}

// ProxyPoolAllocator 为打票和账号转发共享同一组动态关联与容量约束。
type ProxyPoolAllocator struct {
	client         *dbent.Client
	rdb            *redis.Client
	latencyCache   service.ProxyLatencyCache
	settings       proxyPoolSettings
	loadCandidates func(context.Context, service.ProxyPoolSelection) ([]proxyPoolCandidate, error)
}

func NewProxyPoolAllocator(client *dbent.Client, rdb *redis.Client, latencyCache service.ProxyLatencyCache, settings *service.SettingService) *ProxyPoolAllocator {
	a := &ProxyPoolAllocator{client: client, rdb: rdb, latencyCache: latencyCache, settings: settings}
	a.loadCandidates = a.readCandidates
	return a
}

func (a *ProxyPoolAllocator) Select(ctx context.Context, selection service.ProxyPoolSelection) (*service.Proxy, error) {
	if selection.Restricted && len(selection.IDs) == 0 {
		return nil, nil
	}
	if a == nil || a.rdb == nil || a.latencyCache == nil || a.settings == nil || a.loadCandidates == nil {
		return nil, fmt.Errorf("proxy pool allocator dependencies unavailable")
	}
	limit, err := a.settings.GetProxyPoolMaxAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("read proxy pool capacity: %w", err)
	}
	if limit < 0 || limit > 10000 {
		return nil, fmt.Errorf("invalid proxy pool capacity: %d", limit)
	}
	candidates, err := a.loadCandidates(ctx, selection)
	if err != nil {
		return nil, fmt.Errorf("read proxy pool: %w", err)
	}
	return a.reserve(ctx, candidates, selection, limit)
}

func (a *ProxyPoolAllocator) readCandidates(ctx context.Context, selection service.ProxyPoolSelection) ([]proxyPoolCandidate, error) {
	if a.client == nil {
		return nil, fmt.Errorf("proxy pool database unavailable")
	}
	query := a.client.Proxy.Query().Where(dbproxy.StatusEQ(service.StatusActive), dbproxy.DeletedAtIsNil(),
		dbproxy.Or(dbproxy.ExpiresAtIsNil(), dbproxy.ExpiresAtGT(time.Now())))
	if selection.Restricted {
		query.Where(dbproxy.IDIn(selection.IDs...))
	}
	items, err := query.All(ctx)
	if err != nil || len(items) == 0 {
		return nil, err
	}
	ids := make([]int64, len(items))
	for i, item := range items {
		ids[i] = item.ID
	}
	fixed, err := a.readFixedAccounts(ctx, ids)
	if err != nil {
		return nil, err
	}
	candidates := make([]proxyPoolCandidate, len(items))
	for i, item := range items {
		candidates[i] = proxyPoolCandidate{proxy: proxyEntityToService(item), fixedIDs: fixed[item.ID]}
	}
	return candidates, nil
}

func (a *ProxyPoolAllocator) readFixedAccounts(ctx context.Context, ids []int64) (map[int64][]string, error) {
	accounts, err := a.client.Account.Query().Where(dbaccount.DeletedAtIsNil(), dbaccount.ProxyIDIn(ids...)).
		Select(dbaccount.FieldID, dbaccount.FieldProxyID, dbaccount.FieldExtra).All(ctx)
	if err != nil {
		return nil, err
	}
	fixed := make(map[int64][]string)
	for _, account := range accounts {
		mode, _ := account.Extra[service.ProxyModeExtraKey].(string)
		if account.ProxyID == nil || strings.EqualFold(strings.TrimSpace(mode), service.ProxyModeRandom) {
			continue
		}
		fixed[*account.ProxyID] = append(fixed[*account.ProxyID], strconv.FormatInt(account.ID, 10))
	}
	return fixed, nil
}

type proxyPoolLeaseCandidate struct {
	ID       string   `json:"id"`
	Fixed    []string `json:"fixed"`
	Quality  int      `json:"quality"`
	Degraded bool     `json:"degraded"`
	Version  string   `json:"version"`
}

func (a *ProxyPoolAllocator) reserve(ctx context.Context, candidates []proxyPoolCandidate, selection service.ProxyPoolSelection, limit int) (*service.Proxy, error) {
	ids := make([]int64, len(candidates))
	for i, candidate := range candidates {
		ids[i] = candidate.proxy.ID
	}
	health, err := a.latencyCache.GetProxyLatencies(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("read proxy pool health: %w", err)
	}
	// Lua 按输入次序打破同档平局，每次独立打散，避免代理 ID 形成固定优先级。
	rand.Shuffle(len(candidates), func(i, j int) { candidates[i], candidates[j] = candidates[j], candidates[i] })
	keys, leases, proxies := prepareProxyPoolLeases(candidates, health)
	member := strconv.FormatInt(selection.AccountID, 10)
	if selection.AccountID <= 0 {
		member = "request:" + uuid.NewString()
	}
	keys = append(keys, proxyPoolFailureKey(member), proxyPoolAffinityKey(member))
	payload, err := json.Marshal(leases)
	if err != nil {
		return nil, err
	}
	selected, err := proxyPoolReserveScript.Run(ctx, a.rdb, keys, member, int(proxyPoolLeaseTTL.Seconds()), limit,
		string(payload), selection.AccountID > 0, int64(selection.MaxReuseDuration.Seconds())).Text()
	if err != nil {
		return nil, fmt.Errorf("reserve proxy pool association: %w", err)
	}
	return proxies[selected], nil
}

func prepareProxyPoolLeases(candidates []proxyPoolCandidate, health map[int64]*service.ProxyLatencyInfo) ([]string, []proxyPoolLeaseCandidate, map[string]*service.Proxy) {
	keys := make([]string, 0, len(candidates))
	leases := make([]proxyPoolLeaseCandidate, 0, len(candidates))
	proxies := make(map[string]*service.Proxy, len(candidates))
	for _, candidate := range candidates {
		quality, degraded, valid := proxyPoolQuality(candidate.proxy, health[candidate.proxy.ID])
		if !valid {
			continue
		}
		id := strconv.FormatInt(candidate.proxy.ID, 10)
		fixed := append([]string{}, candidate.fixedIDs...)
		keys = append(keys, proxyPoolLeaseKey(id))
		version := fmt.Sprintf("%x", sha256.Sum256([]byte(candidate.proxy.URL())))
		leases = append(leases, proxyPoolLeaseCandidate{ID: id, Fixed: fixed, Quality: quality, Degraded: degraded, Version: version})
		proxies[id] = candidate.proxy
	}
	return keys, leases, proxies
}

func proxyPoolLeaseKey(id string) string {
	return "proxy:{pool}:associations:" + id
}

func proxyPoolAffinityKey(member string) string {
	return "proxy:{pool}:affinity:" + member
}

func proxyPoolQuality(proxy *service.Proxy, info *service.ProxyLatencyInfo) (quality int, degraded, valid bool) {
	if proxy == nil || !proxy.IsActive() || proxy.IsExpired(time.Now()) {
		return 0, false, false
	}
	// 修改地址、凭据或配置后，旧探测不能继续决定新代理的可用性。
	if info == nil || info.UpdatedAt.Before(proxy.UpdatedAt) {
		return 2000, false, true
	}
	if !info.Success {
		return 0, false, false
	}
	degraded = info.QualityStatus == "failed" || info.QualityStatus == "challenge"
	quality = 1000
	if info.QualityStatus == "healthy" || info.QualityGrade == "A" || info.QualityGrade == "B" {
		quality = 100
	}
	if info.QualityScore != nil {
		quality += 100 - max(0, min(100, *info.QualityScore))
	}
	return quality, degraded, true
}
