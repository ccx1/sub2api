package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type codexTicketSettingsReader interface {
	GetCodexTicketSettings(context.Context) (config.OpenAICodexTicketConfig, error)
}

type codexTicketDisabledAtReader interface {
	GetCodexTicketProtectionDisabledAt(context.Context) (time.Time, error)
}

type codexSchedulerCandidate struct {
	ID              string   `json:"id"`
	Version         string   `json:"version"`
	Fixed           []string `json:"fixed"`
	Healthy         bool     `json:"healthy"`
	Degraded        bool     `json:"degraded"`
	Quality         int      `json:"quality"`
	AffinityVersion string   `json:"affinity_version"`
}

func codexSchedulerAccountKey(id int64) string {
	return "proxy:{pool}:codex:account:" + strconv.FormatInt(id, 10)
}

func codexSchedulerVersion(value any) string {
	encoded, _ := json.Marshal(value)
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func (a *ProxyPoolAllocator) codexSchedulerConfig(ctx context.Context, fallback config.OpenAICodexTicketConfig) (config.OpenAICodexTicketConfig, error) {
	if a == nil || a.rdb == nil || a.settings == nil {
		return fallback, errors.New("codex ticket shared scheduler unavailable")
	}
	if source, ok := a.settings.(codexTicketSettingsReader); ok {
		return source.GetCodexTicketSettings(ctx)
	}
	return config.NormalizeOpenAICodexTicketConfig(fallback), nil
}

func (a *ProxyPoolAllocator) codexSchedulerInput(ctx context.Context, action string, cfg config.OpenAICodexTicketConfig) (map[string]any, error) {
	cfg = config.NormalizeOpenAICodexTicketConfig(cfg)
	p := cfg.TicketProtection()
	// 账号可单独启用 Cookie；普通采集还需预留一次候选 Cookie 复验。
	probeCount := 3
	if config.CodexTicketBusinessVerificationEnabled(cfg) && cfg.BusinessVerificationRounds > 1 {
		// 采集、同出口质量复验及可能存在的独立业务出口复验共用一次租约。
		probeCount = cfg.BusinessVerificationRounds + 2
	}
	var disabledAt time.Time
	if source, ok := a.settings.(codexTicketDisabledAtReader); ok {
		var err error
		disabledAt, err = source.GetCodexTicketProtectionDisabledAt(ctx)
		if err != nil {
			return nil, errors.New("read codex ticket protection transition failed")
		}
	}
	return map[string]any{
		"action": action, "enabled": p.Enabled, "policy": codexSchedulerVersion(cfg),
		"disabled_at_ms": codexSchedulerMillis(disabledAt),
		"max_attempts":   max(1, p.MaxAccountAttempts), "max_rounds": max(1, p.MaxPoolRounds),
		"cooldown_ms": max(1, p.AccountCooldownSeconds) * 1000, "silence_ms": max(1, p.ProxySilenceSeconds) * 1000,
		"interval_ms": max(1, cfg.HarvestProbeIntervalSeconds) * 1000,
		"lease_ms":    (probeCount*cfg.HarvestAttemptTimeoutSeconds + 30) * 1000,
		"backoff":     cfg.RetryBackoffSeconds, "retry_max": cfg.RetryMaxAttempts,
		"retry_exhausted_ms":    cfg.RetryExhaustedCooldownSeconds * 1000,
		"rejection_interval_ms": p.RejectionRetryIntervalSeconds * 1000,
		"rejection_max":         p.RejectionRetryMaxAttempts,
		"rejection_cooldown_ms": p.RejectionRetryCooldownSeconds * 1000,
		"pin_threshold":         max(1, cfg.ProxyFailureThreshold),
		"session_mode":          cfg.SessionMode, "random_seed": uuid.NewString(), "next_epoch": uuid.NewString(),
		"business_lease_seconds": int64(proxyPoolLeaseTTL.Seconds()),
	}, nil
}

func (a *ProxyPoolAllocator) ReserveCodexTicket(ctx context.Context, req service.CodexTicketReserveRequest) (*service.CodexTicketReservation, error) {
	req.Model = strings.TrimSpace(req.Model)
	if req.AccountID <= 0 || strings.TrimSpace(req.Model) == "" {
		return nil, errors.New("invalid codex ticket scheduling identity")
	}
	cfg, err := a.codexSchedulerConfig(ctx, req.Config)
	if err != nil {
		return nil, err
	}
	q, err := a.codexSchedulerInput(ctx, "reserve", cfg)
	if err != nil {
		return nil, err
	}
	if q["policy"] != codexSchedulerVersion(config.NormalizeOpenAICodexTicketConfig(req.Config)) {
		return nil, codexSchedulerWait("controls_changed")
	}
	candidates, proxies, err := a.codexSchedulerCandidates(ctx, req)
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 && req.AllowDirectOnEmpty && req.HarvestUsesBusiness {
		candidates = []codexSchedulerCandidate{{ID: "0", Version: "direct", Healthy: true, Fixed: []string{}}}
	}
	limit, err := a.settings.GetProxyPoolMaxAccounts(ctx)
	if err != nil || limit < 0 || limit > 10000 {
		return nil, errors.New("read codex ticket proxy capacity failed")
	}
	models := append([]string(nil), req.Models...)
	if len(models) == 0 {
		models = []string{req.Model}
	}
	models = uniqueCodexSchedulerModels(models)
	q["account"], q["model"], q["models"] = strconv.FormatInt(req.AccountID, 10), req.Model, models
	q["candidates"], q["limit"], q["pool"], q["manual"] = candidates, limit, req.PoolMode, req.Manual
	q["strategy"], q["token"] = req.Strategy, uuid.NewString()
	q["follow_business"] = req.HarvestUsesBusiness
	q["pool_version"] = codexSchedulerVersion(candidatesPoolVersion(candidates))
	if err := a.codexSchedulerDeferredProxy(ctx, req, q); err != nil {
		return nil, err
	}
	result, err := a.runCodexScheduler(ctx, req.AccountID, q)
	if err != nil {
		return nil, err
	}
	return &service.CodexTicketReservation{AccountID: req.AccountID, Model: req.Model, Token: result.Token,
		Generation: result.Status.Generation, Proxy: proxies[result.ProxyID], HalfOpen: result.Status.HalfOpen,
		Manual: req.Manual, Status: result.Status, Config: cfg, SessionEpoch: result.SessionEpoch}, nil
}

func (a *ProxyPoolAllocator) codexSchedulerDeferredProxy(ctx context.Context, req service.CodexTicketReserveRequest, q map[string]any) error {
	if a.client == nil {
		return nil
	}
	raw, err := a.rdb.Get(ctx, codexSchedulerAccountKey(req.AccountID)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil
		}
		return errors.New("codex ticket shared scheduler unavailable")
	}
	var state struct {
		ProxyID string `json:"deferredproxy"`
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return errors.New("invalid codex ticket shared state")
	}
	proxyID, _ := strconv.ParseInt(state.ProxyID, 10, 64)
	if proxyID <= 0 {
		return nil
	}
	entity, err := a.client.Proxy.Get(ctx, proxyID)
	if err != nil {
		if dbent.IsNotFound(err) {
			q["deferred_invalid"] = true
			return nil
		}
		return errors.New("read deferred codex ticket proxy failed")
	}
	proxy := proxyEntityToService(entity)
	if !proxy.IsActive() || proxy.IsExpired(time.Now()) {
		q["deferred_invalid"] = true
		return nil
	}
	candidates, _, err := a.codexSchedulerCandidates(ctx, service.CodexTicketReserveRequest{
		FixedProxy: proxy, Selection: service.ProxyPoolSelection{CountryCode: req.Selection.CountryCode},
	})
	if err != nil {
		return err
	}
	if len(candidates) > 0 {
		q["deferred_candidate"] = candidates[0]
	} else if req.Selection.CountryCode != "" {
		q["deferred_invalid"] = true
	}
	return nil
}

func uniqueCodexSchedulerModels(models []string) []string {
	set := make(map[string]bool)
	result := make([]string, 0, len(models))
	for _, model := range models {
		if model = strings.TrimSpace(model); model != "" && !set[model] {
			set[model] = true
			result = append(result, model)
		}
	}
	sort.Strings(result)
	return result
}

func candidatesPoolVersion(candidates []codexSchedulerCandidate) []string {
	result := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		result = append(result, candidate.ID+":"+candidate.Version)
	}
	return result
}

func (a *ProxyPoolAllocator) codexSchedulerCandidates(ctx context.Context, req service.CodexTicketReserveRequest) ([]codexSchedulerCandidate, map[string]*service.Proxy, error) {
	items := []proxyPoolCandidate{}
	if req.PoolMode {
		if req.Selection.Restricted && len(req.Selection.IDs) == 0 {
			return []codexSchedulerCandidate{}, map[string]*service.Proxy{}, nil
		}
		if a.loadCandidates == nil || a.latencyCache == nil {
			return nil, nil, errors.New("codex ticket proxy pool unavailable")
		}
		var err error
		items, err = a.loadCandidates(ctx, req.Selection)
		if err != nil {
			return nil, nil, err
		}
	} else if req.FixedProxy != nil && req.FixedProxy.ID > 0 {
		fixed := []string{}
		if a.client != nil {
			bindings, err := a.readFixedAccounts(ctx, []int64{req.FixedProxy.ID})
			if err != nil {
				return nil, nil, err
			}
			fixed = bindings[req.FixedProxy.ID]
		}
		items = append(items, proxyPoolCandidate{proxy: req.FixedProxy, fixedIDs: fixed})
	} else {
		if req.Selection.CountryCode != "" {
			return nil, nil, errors.New("proxy region cannot be verified for direct ticket egress")
		}
		return []codexSchedulerCandidate{{ID: "0", Version: "direct", Healthy: true, Fixed: []string{}}}, map[string]*service.Proxy{}, nil
	}
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if item.proxy != nil {
			ids = append(ids, item.proxy.ID)
		}
	}
	health := map[int64]*service.ProxyLatencyInfo{}
	if req.PoolMode || req.Selection.CountryCode != "" {
		if a.latencyCache == nil {
			return nil, nil, errors.New("codex ticket proxy region cache unavailable")
		}
		var err error
		health, err = a.latencyCache.GetProxyLatencies(ctx, ids)
		if err != nil {
			return nil, nil, err
		}
	}
	result, proxies := []codexSchedulerCandidate{}, map[string]*service.Proxy{}
	for _, item := range items {
		proxy := item.proxy
		if proxy == nil || !proxy.IsActive() || proxy.IsExpired(time.Now()) {
			continue
		}
		if req.PoolMode && req.Selection.Restricted && !containsCodexProxyID(req.Selection.IDs, proxy.ID) {
			continue
		}
		if !proxyMatchesRegion(proxy, health[proxy.ID], req.Selection.CountryCode) &&
			!(req.PoolMode && codexSchedulerRegionPending(proxy, health[proxy.ID], req.Selection.CountryCode)) {
			continue
		}
		quality, degraded, healthy := proxyPoolQuality(proxy, health[proxy.ID])
		id := strconv.FormatInt(proxy.ID, 10)
		result = append(result, codexSchedulerCandidate{ID: id, Version: codexSchedulerVersion(proxy.URL()), Fixed: append([]string{}, item.fixedIDs...), Healthy: healthy, Degraded: degraded,
			Quality: quality, AffinityVersion: fmt.Sprintf("%x", sha256.Sum256([]byte(proxy.URL())))})
		proxies[id] = proxy
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, proxies, nil
}

// 地区元数据仍有效但本次健康探测失败时保留等待候选，避免误判空池而直连。
func codexSchedulerRegionPending(proxy *service.Proxy, info *service.ProxyLatencyInfo, country string) bool {
	return country != "" && service.ProxyLatencyMatchesProxy(info, proxy) && !info.Success &&
		strings.EqualFold(strings.TrimSpace(info.CountryCode), country)
}

func containsCodexProxyID(ids []int64, target int64) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func codexSchedulerWait(reason string) error {
	return &service.CodexTicketWaitError{Status: &service.CodexTicketRuntimeStatus{State: "waiting", Reason: reason}}
}
