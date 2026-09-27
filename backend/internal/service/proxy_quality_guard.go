package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/google/uuid"
)

const (
	ProxyQualityGuardStateActive   = "active"
	ProxyQualityGuardStateDisabled = "disabled"
	ProxyQualityGuardStateDeleted  = "deleted"

	ProxyQualityGuardActionCheckFailed = "check_failed"
	ProxyQualityGuardActionDisabled    = "disabled"
	ProxyQualityGuardActionRestored    = "restored"
	ProxyQualityGuardActionDeleted     = "deleted"
	ProxyQualityGuardActionDeleteSkip  = "delete_skipped"
	ProxyQualityGuardActionReset       = "reset"

	proxyQualityGuardLeaderLockKey = "proxy_quality_guard:leader"
	proxyQualityGuardLeaderLockTTL = 10 * time.Minute
	proxyQualityGuardCheckTimeout  = 90 * time.Second
	proxyQualityGuardEventLimit    = 200
)

// ProxyQualityGuardState 是单个代理在巡检中的自动禁用轮数与最近检测结论。
type ProxyQualityGuardState struct {
	ProxyID             int64      `json:"proxy_id"`
	State               string     `json:"state"`
	Rounds              int        `json:"rounds"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	DisabledUntil       *time.Time `json:"disabled_until,omitempty"`
	LastCheckedAt       *time.Time `json:"last_checked_at,omitempty"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastFailedAt        *time.Time `json:"last_failed_at,omitempty"`
	LastScore           *int       `json:"last_score,omitempty"`
	LastGrade           string     `json:"last_grade"`
	LastError           string     `json:"last_error"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type ProxyQualityGuardEvent struct {
	ID        int64     `json:"id"`
	ProxyID   int64     `json:"proxy_id"`
	ProxyName string    `json:"proxy_name"`
	Action    string    `json:"action"`
	Round     int       `json:"round"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

// ProxyQualityGuardCandidate 是可被巡检接管的公共池代理；私有共享代理不在此列。
type ProxyQualityGuardCandidate struct {
	Proxy             Proxy
	FixedBoundCount   int64
	DynamicBoundCount int64
}

// ProxyQualityGuardItem 汇总代理、健康缓存与巡检状态，供质量管理页展示。
type ProxyQualityGuardItem struct {
	ProxyID             int64      `json:"proxy_id"`
	Name                string     `json:"name"`
	Protocol            string     `json:"protocol"`
	Host                string     `json:"host"`
	Port                int        `json:"port"`
	GroupName           string     `json:"group_name,omitempty"`
	ProxyStatus         string     `json:"proxy_status"`
	State               string     `json:"state"`
	Rounds              int        `json:"rounds"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	DisabledUntil       *time.Time `json:"disabled_until,omitempty"`
	LastCheckedAt       *time.Time `json:"last_checked_at,omitempty"`
	LastSuccessAt       *time.Time `json:"last_success_at,omitempty"`
	LastError           string     `json:"last_error"`
	QualityScore        *int       `json:"quality_score,omitempty"`
	QualityGrade        string     `json:"quality_grade"`
	QualityStatus       string     `json:"quality_status"`
	LatencyMs           *int64     `json:"latency_ms,omitempty"`
	IPAddress           string     `json:"ip_address,omitempty"`
	CountryCode         string     `json:"country_code,omitempty"`
	FixedBoundCount     int64      `json:"fixed_bound_count"`
	DynamicBoundCount   int64      `json:"dynamic_bound_count"`
	PreUseReady         bool       `json:"pre_use_ready"`
	Managed             bool       `json:"managed"`
}

type ProxyQualityGuardSummary struct {
	Total    int `json:"total"`
	Healthy  int `json:"healthy"`
	Pending  int `json:"pending"`
	Failing  int `json:"failing"`
	Disabled int `json:"disabled"`
}

type ProxyQualityGuardOverview struct {
	Settings ProxyQualityGuardSettings `json:"settings"`
	Summary  ProxyQualityGuardSummary  `json:"summary"`
	Items    []ProxyQualityGuardItem   `json:"items"`
	LastRun  *time.Time                `json:"last_run,omitempty"`
}

type ProxyQualityGuardRunResult struct {
	Checked  int `json:"checked"`
	Failed   int `json:"failed"`
	Disabled int `json:"disabled"`
	Restored int `json:"restored"`
	Deleted  int `json:"deleted"`
}

// ProxyQualityGuardRepository 在同一事务内切换代理状态与巡检状态，避免禁用后丢失轮数。
type ProxyQualityGuardRepository interface {
	ListProxyQualityGuardCandidates(ctx context.Context) ([]ProxyQualityGuardCandidate, error)
	ListProxyQualityGuardStates(ctx context.Context) (map[int64]*ProxyQualityGuardState, error)
	SaveProxyQualityGuardState(ctx context.Context, state *ProxyQualityGuardState) error
	DisableProxyForQualityGuard(ctx context.Context, state *ProxyQualityGuardState, event ProxyQualityGuardEvent) (bool, error)
	RestoreProxyForQualityGuard(ctx context.Context, state *ProxyQualityGuardState, event ProxyQualityGuardEvent) (bool, error)
	DeleteProxyForQualityGuard(ctx context.Context, state *ProxyQualityGuardState, event ProxyQualityGuardEvent) (bool, error)
	ResetProxyQualityGuardState(ctx context.Context, proxyID int64, event ProxyQualityGuardEvent) error
	AppendProxyQualityGuardEvent(ctx context.Context, event ProxyQualityGuardEvent) error
	ListProxyQualityGuardEvents(ctx context.Context, limit int) ([]ProxyQualityGuardEvent, error)
	CountProxyQualityGuardRuntimeFailures(ctx context.Context, proxyIDs []int64) (map[int64]int64, error)
	ClearProxyQualityGuardRuntimeFailures(ctx context.Context, proxyID int64) error
}

type ProxyQualityGuardService struct {
	store        ProxyQualityGuardRepository
	settings     SettingRepository
	prober       ProxyExitInfoProber
	latencyCache ProxyLatencyCache
	lockCache    LeaderLockCache
	db           *sql.DB
	instanceID   string
	now          func() time.Time
	check        func(context.Context, *Proxy, ProxyQualityGuardSettings) proxyQualityGuardCheck

	runMu     sync.Mutex
	lastRun   *time.Time
	stopCh    chan struct{}
	wakeCh    chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup
}

type proxyQualityGuardCheck struct {
	passed bool
	score  *int
	grade  string
	reason string
}

func NewProxyQualityGuardService(store ProxyQualityGuardRepository, settings SettingRepository, prober ProxyExitInfoProber,
	latencyCache ProxyLatencyCache, lockCache LeaderLockCache, db *sql.DB) *ProxyQualityGuardService {
	s := &ProxyQualityGuardService{store: store, settings: settings, prober: prober, latencyCache: latencyCache,
		lockCache: lockCache, db: db, instanceID: uuid.NewString(), now: time.Now,
		stopCh: make(chan struct{}), wakeCh: make(chan struct{}, 1)}
	s.check = s.checkProxy
	return s
}

// 代理仓储在生产环境同时实现巡检事务；测试桩未实现时服务保持空转。
func ProvideProxyQualityGuardService(proxyRepo ProxyRepository, settings SettingRepository, prober ProxyExitInfoProber,
	latencyCache ProxyLatencyCache, lockCache LeaderLockCache, db *sql.DB) *ProxyQualityGuardService {
	store, _ := proxyRepo.(ProxyQualityGuardRepository)
	svc := NewProxyQualityGuardService(store, settings, prober, latencyCache, lockCache, db)
	svc.Start()
	return svc
}

func (s *ProxyQualityGuardService) Start() {
	if s == nil || s.store == nil {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go s.run()
	})
}

func (s *ProxyQualityGuardService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

func (s *ProxyQualityGuardService) run() {
	defer s.wg.Done()
	timer := time.NewTimer(s.tickInterval())
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			s.runScheduled()
		case <-s.wakeCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			s.runScheduled()
		case <-s.stopCh:
			return
		}
		timer.Reset(s.tickInterval())
	}
}

func (s *ProxyQualityGuardService) tickInterval() time.Duration {
	return time.Duration(s.currentSettings().IntervalSeconds) * time.Second
}

func (s *ProxyQualityGuardService) runScheduled() {
	cfg := s.currentSettings()
	if !cfg.Enabled {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), proxyQualityGuardLeaderLockTTL-time.Minute)
	defer cancel()
	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, proxyQualityGuardLeaderLockKey, s.instanceID, proxyQualityGuardLeaderLockTTL)
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}
	if _, err := s.runCycle(ctx, cfg, false); err != nil {
		slog.Warn("proxy_quality_guard.run_failed", "error", err)
	}
}

// RunNow 由管理员手动触发：忽略复检间隔，但仍遵守单实例互斥与当前配置。
func (s *ProxyQualityGuardService) RunNow(ctx context.Context) (*ProxyQualityGuardRunResult, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("proxy quality guard repository unavailable")
	}
	release, ok := tryAcquireSingletonLeaderLock(ctx, s.lockCache, s.db, proxyQualityGuardLeaderLockKey, s.instanceID, proxyQualityGuardLeaderLockTTL)
	if !ok {
		return nil, ErrProxyQualityGuardBusy
	}
	if release != nil {
		defer release()
	}
	return s.runCycle(ctx, s.currentSettings(), true)
}

var ErrProxyQualityGuardBusy = infraerrors.Conflict("PROXY_QUALITY_GUARD_BUSY", "proxy quality guard is already running")

func (s *ProxyQualityGuardService) runCycle(ctx context.Context, cfg ProxyQualityGuardSettings, force bool) (*ProxyQualityGuardRunResult, error) {
	s.runMu.Lock()
	defer s.runMu.Unlock()
	result := &ProxyQualityGuardRunResult{}
	candidates, err := s.store.ListProxyQualityGuardCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list proxy quality guard candidates: %w", err)
	}
	states, err := s.store.ListProxyQualityGuardStates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list proxy quality guard states: %w", err)
	}
	now := s.now()
	due := s.dueCandidates(ctx, candidates, states, cfg, now, force)
	outcomes := s.checkAll(ctx, due, cfg)
	for i, candidate := range due {
		if ctx.Err() != nil {
			break
		}
		state := states[candidate.Proxy.ID]
		if state == nil {
			state = &ProxyQualityGuardState{ProxyID: candidate.Proxy.ID, State: ProxyQualityGuardStateActive}
		}
		s.apply(ctx, candidate, state, outcomes[i], cfg, result)
	}
	finished := s.now()
	s.lastRun = &finished
	return result, nil
}

// dueCandidates 按“冷却到期的禁用代理优先、最久未检测其次”排序，并受单轮上限约束。
func (s *ProxyQualityGuardService) dueCandidates(ctx context.Context, candidates []ProxyQualityGuardCandidate,
	states map[int64]*ProxyQualityGuardState, cfg ProxyQualityGuardSettings, now time.Time, force bool) []ProxyQualityGuardCandidate {
	runtimeFailures := map[int64]int64{}
	if cfg.RuntimeFailureThreshold > 0 {
		ids := make([]int64, 0, len(candidates))
		for _, candidate := range candidates {
			ids = append(ids, candidate.Proxy.ID)
		}
		if counts, err := s.store.CountProxyQualityGuardRuntimeFailures(ctx, ids); err == nil {
			runtimeFailures = counts
		} else {
			slog.Warn("proxy_quality_guard.runtime_failures_failed", "error", err)
		}
	}
	type ranked struct {
		candidate ProxyQualityGuardCandidate
		priority  int
		checkedAt time.Time
	}
	items := make([]ranked, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.FixedBoundCount > 0 && !cfg.IncludeFixedBound {
			continue
		}
		state := states[candidate.Proxy.ID]
		checkedAt := time.Time{}
		if state != nil && state.LastCheckedAt != nil {
			checkedAt = *state.LastCheckedAt
		}
		switch {
		case state != nil && state.State == ProxyQualityGuardStateDisabled:
			// 管理员手动改回启用时视作人工接管，跳过自动恢复并重新按周期巡检。
			if candidate.Proxy.Status == StatusActive {
				items = append(items, ranked{candidate: candidate, priority: 2, checkedAt: checkedAt})
				continue
			}
			if state.DisabledUntil == nil || !state.DisabledUntil.After(now) {
				items = append(items, ranked{candidate: candidate, priority: 0, checkedAt: checkedAt})
			}
		case candidate.Proxy.Status != StatusActive:
			// 非巡检禁用的停用代理由管理员维护，不自动检测或恢复。
			continue
		case force || state == nil || state.LastCheckedAt == nil:
			items = append(items, ranked{candidate: candidate, priority: 1, checkedAt: checkedAt})
		case cfg.RuntimeFailureThreshold > 0 && runtimeFailures[candidate.Proxy.ID] >= int64(cfg.RuntimeFailureThreshold):
			items = append(items, ranked{candidate: candidate, priority: 1, checkedAt: checkedAt})
		case !checkedAt.Add(time.Duration(cfg.CheckIntervalMinutes) * time.Minute).After(now):
			items = append(items, ranked{candidate: candidate, priority: 2, checkedAt: checkedAt})
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].priority != items[j].priority {
			return items[i].priority < items[j].priority
		}
		return items[i].checkedAt.Before(items[j].checkedAt)
	})
	if len(items) > cfg.MaxChecksPerRun {
		items = items[:cfg.MaxChecksPerRun]
	}
	out := make([]ProxyQualityGuardCandidate, len(items))
	for i := range items {
		out[i] = items[i].candidate
	}
	return out
}

func (s *ProxyQualityGuardService) checkAll(ctx context.Context, due []ProxyQualityGuardCandidate, cfg ProxyQualityGuardSettings) []proxyQualityGuardCheck {
	outcomes := make([]proxyQualityGuardCheck, len(due))
	sem := make(chan struct{}, cfg.Concurrency)
	var wg sync.WaitGroup
	for i := range due {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				outcomes[i] = proxyQualityGuardCheck{reason: ctx.Err().Error()}
				return
			}
			defer func() { <-sem }()
			// 以启用态身份检测，确保写入的健康缓存在恢复后仍与代理匹配。
			probe := due[i].Proxy
			probe.Status = StatusActive
			checkCtx, cancel := context.WithTimeout(ctx, proxyQualityGuardCheckTimeout)
			defer cancel()
			outcomes[i] = s.check(checkCtx, &probe, cfg)
		}(i)
	}
	wg.Wait()
	return outcomes
}

func (s *ProxyQualityGuardService) checkProxy(ctx context.Context, proxy *Proxy, cfg ProxyQualityGuardSettings) proxyQualityGuardCheck {
	if cfg.CheckMode == ProxyQualityGuardCheckModeBasic {
		return s.checkProxyBasic(ctx, proxy)
	}
	result, exitInfo := runProxyQualityCheck(ctx, s.prober, proxy)
	info := buildProxyQualitySnapshot(proxy, result, exitInfo)
	storeProxyLatency(ctx, s.latencyCache, proxy.ID, info)
	score := result.Score
	outcome := proxyQualityGuardCheck{score: &score, grade: result.Grade, passed: cfg.PassesPreUseCheck(proxy, info)}
	if !outcome.passed {
		outcome.reason = proxyQualityGuardFailureReason(result, cfg)
	}
	return outcome
}

func (s *ProxyQualityGuardService) checkProxyBasic(ctx context.Context, proxy *Proxy) proxyQualityGuardCheck {
	if s.prober == nil {
		return proxyQualityGuardCheck{reason: "代理探测服务未配置"}
	}
	exitInfo, latencyMs, err := s.prober.ProbeProxy(ctx, proxy.URL())
	info := &ProxyLatencyInfo{ProxyIdentity: ProxyProbeIdentity(proxy), UpdatedAt: time.Now()}
	if err != nil {
		info.Message = err.Error()
		storeProxyLatency(ctx, s.latencyCache, proxy.ID, info)
		return proxyQualityGuardCheck{reason: "连通性检测失败: " + err.Error()}
	}
	info.Success = true
	info.LatencyMs = &latencyMs
	info.Message = "Proxy is accessible"
	info.IPAddress, info.Country, info.CountryCode = exitInfo.IP, exitInfo.Country, exitInfo.CountryCode
	info.Region, info.City = exitInfo.Region, exitInfo.City
	storeProxyLatency(ctx, s.latencyCache, proxy.ID, info)
	return proxyQualityGuardCheck{passed: true}
}

func proxyQualityGuardFailureReason(result *ProxyQualityCheckResult, cfg ProxyQualityGuardSettings) string {
	if !proxyQualityBaseConnectivityPass(result) {
		for _, item := range result.Items {
			if item.Status != "pass" {
				return "连通性检测失败: " + item.Message
			}
		}
		return "连通性检测失败"
	}
	if cfg.FailOnChallenge && result.ChallengeCount > 0 {
		return fmt.Sprintf("触发风控挑战（%s）", result.Summary)
	}
	return fmt.Sprintf("质量分 %d 低于阈值 %d（%s）", result.Score, cfg.MinScore, result.Summary)
}

// apply 推进单个代理的状态机：active →(连续失败达阈值) disabled →(冷却到期复检通过) active，
// 超过最大轮数后删除；删除条件不满足时保持禁用并记录原因。
func (s *ProxyQualityGuardService) apply(ctx context.Context, candidate ProxyQualityGuardCandidate, state *ProxyQualityGuardState,
	outcome proxyQualityGuardCheck, cfg ProxyQualityGuardSettings, result *ProxyQualityGuardRunResult) {
	now := s.now()
	proxy := candidate.Proxy
	result.Checked++
	state.LastCheckedAt = &now
	state.LastScore = outcome.score
	state.LastGrade = outcome.grade
	manualOverride := state.State == ProxyQualityGuardStateDisabled && proxy.Status == StatusActive
	if manualOverride {
		state.State = ProxyQualityGuardStateActive
		state.DisabledUntil = nil
	}
	if state.State == "" {
		state.State = ProxyQualityGuardStateActive
	}
	if cfg.StableResetHours > 0 && state.Rounds > 0 && state.LastFailedAt != nil &&
		now.Sub(*state.LastFailedAt) >= time.Duration(cfg.StableResetHours)*time.Hour {
		state.Rounds = 0
	}

	if outcome.passed {
		state.ConsecutiveFailures = 0
		state.LastSuccessAt = &now
		state.LastError = ""
		if err := s.store.ClearProxyQualityGuardRuntimeFailures(ctx, proxy.ID); err != nil {
			slog.Warn("proxy_quality_guard.clear_runtime_failures_failed", "proxy_id", proxy.ID, "error", err)
		}
		if state.State == ProxyQualityGuardStateDisabled {
			state.State = ProxyQualityGuardStateActive
			state.DisabledUntil = nil
			changed, err := s.store.RestoreProxyForQualityGuard(ctx, state, s.event(proxy, ProxyQualityGuardActionRestored, state.Rounds, "复检通过，恢复启用"))
			if err != nil {
				slog.Warn("proxy_quality_guard.restore_failed", "proxy_id", proxy.ID, "error", err)
				return
			}
			if changed {
				result.Restored++
			}
			return
		}
		s.saveState(ctx, state)
		return
	}

	result.Failed++
	state.ConsecutiveFailures++
	state.LastFailedAt = &now
	state.LastError = outcome.reason
	wasDisabled := state.State == ProxyQualityGuardStateDisabled
	if !wasDisabled && state.ConsecutiveFailures < cfg.FailureThreshold {
		s.saveState(ctx, state)
		s.appendEvent(ctx, s.event(proxy, ProxyQualityGuardActionCheckFailed, state.Rounds,
			fmt.Sprintf("%s（%d/%d）", outcome.reason, state.ConsecutiveFailures, cfg.FailureThreshold)))
		return
	}

	state.Rounds++
	state.ConsecutiveFailures = 0
	if state.Rounds >= cfg.MaxRounds && cfg.AutoDelete {
		if s.tryDelete(ctx, candidate, state, outcome.reason, cfg, result) {
			return
		}
	}
	until := now.Add(time.Duration(cfg.DisableMinutes) * time.Minute)
	state.State = ProxyQualityGuardStateDisabled
	state.DisabledUntil = &until
	reason := fmt.Sprintf("%s，第 %d/%d 轮禁用 %d 分钟", outcome.reason, state.Rounds, cfg.MaxRounds, cfg.DisableMinutes)
	changed, err := s.store.DisableProxyForQualityGuard(ctx, state, s.event(proxy, ProxyQualityGuardActionDisabled, state.Rounds, reason))
	if err != nil {
		slog.Warn("proxy_quality_guard.disable_failed", "proxy_id", proxy.ID, "error", err)
		return
	}
	if changed && !wasDisabled {
		result.Disabled++
	}
	slog.Warn("proxy_quality_guard.proxy_disabled", "proxy_id", proxy.ID, "round", state.Rounds, "reason", outcome.reason)
}

func (s *ProxyQualityGuardService) tryDelete(ctx context.Context, candidate ProxyQualityGuardCandidate, state *ProxyQualityGuardState,
	reason string, cfg ProxyQualityGuardSettings, result *ProxyQualityGuardRunResult) bool {
	proxy := candidate.Proxy
	if candidate.FixedBoundCount > 0 {
		// 固定绑定账号的代理不能删除（与手动删除规则一致），只保持禁用等待人工处理。
		s.appendEvent(ctx, s.event(proxy, ProxyQualityGuardActionDeleteSkip, state.Rounds,
			fmt.Sprintf("已达 %d 轮，但仍有 %d 个账号固定绑定，保持禁用", cfg.MaxRounds, candidate.FixedBoundCount)))
		return false
	}
	state.State = ProxyQualityGuardStateDeleted
	state.DisabledUntil = nil
	changed, err := s.store.DeleteProxyForQualityGuard(ctx, state, s.event(proxy, ProxyQualityGuardActionDeleted, state.Rounds,
		fmt.Sprintf("%s，已达 %d 轮，自动删除", reason, cfg.MaxRounds)))
	if err != nil {
		state.State = ProxyQualityGuardStateDisabled
		reasonText := err.Error()
		if errors.Is(err, ErrProxyInUse) {
			reasonText = "代理仍被账号固定绑定"
		}
		s.appendEvent(ctx, s.event(proxy, ProxyQualityGuardActionDeleteSkip, state.Rounds, "自动删除失败: "+reasonText))
		return false
	}
	if changed {
		result.Deleted++
		slog.Warn("proxy_quality_guard.proxy_deleted", "proxy_id", proxy.ID, "rounds", state.Rounds)
	}
	return true
}

func (s *ProxyQualityGuardService) event(proxy Proxy, action string, round int, reason string) ProxyQualityGuardEvent {
	return ProxyQualityGuardEvent{ProxyID: proxy.ID, ProxyName: proxy.Name, Action: action, Round: round, Reason: reason, CreatedAt: s.now()}
}

func (s *ProxyQualityGuardService) saveState(ctx context.Context, state *ProxyQualityGuardState) {
	if err := s.store.SaveProxyQualityGuardState(ctx, state); err != nil {
		slog.Warn("proxy_quality_guard.save_state_failed", "proxy_id", state.ProxyID, "error", err)
	}
}

func (s *ProxyQualityGuardService) appendEvent(ctx context.Context, event ProxyQualityGuardEvent) {
	if err := s.store.AppendProxyQualityGuardEvent(ctx, event); err != nil {
		slog.Warn("proxy_quality_guard.append_event_failed", "proxy_id", event.ProxyID, "error", err)
	}
}

func (s *ProxyQualityGuardService) GetSettings(_ context.Context) ProxyQualityGuardSettings {
	return s.currentSettings()
}

func (s *ProxyQualityGuardService) UpdateSettings(ctx context.Context, in ProxyQualityGuardSettings) (ProxyQualityGuardSettings, error) {
	if s == nil || s.settings == nil {
		return DefaultProxyQualityGuardSettings(), errors.New("proxy quality guard settings unavailable")
	}
	norm := normalizeProxyQualityGuardSettings(in)
	raw, err := json.Marshal(norm)
	if err != nil {
		return norm, err
	}
	if err := s.settings.Set(ctx, SettingKeyProxyQualityGuardSettings, string(raw)); err != nil {
		return norm, err
	}
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
	return norm, nil
}

func (s *ProxyQualityGuardService) Events(ctx context.Context) ([]ProxyQualityGuardEvent, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("proxy quality guard repository unavailable")
	}
	return s.store.ListProxyQualityGuardEvents(ctx, proxyQualityGuardEventLimit)
}

// Reset 清空代理的自动禁用轮数；若代理由巡检禁用则一并恢复启用。
func (s *ProxyQualityGuardService) Reset(ctx context.Context, proxyID int64) error {
	if s == nil || s.store == nil {
		return errors.New("proxy quality guard repository unavailable")
	}
	if err := s.store.ClearProxyQualityGuardRuntimeFailures(ctx, proxyID); err != nil {
		slog.Warn("proxy_quality_guard.clear_runtime_failures_failed", "proxy_id", proxyID, "error", err)
	}
	return s.store.ResetProxyQualityGuardState(ctx, proxyID, ProxyQualityGuardEvent{
		ProxyID: proxyID, Action: ProxyQualityGuardActionReset, Reason: "管理员重置巡检状态", CreatedAt: s.now(),
	})
}

func (s *ProxyQualityGuardService) Overview(ctx context.Context) (*ProxyQualityGuardOverview, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("proxy quality guard repository unavailable")
	}
	cfg := s.currentSettings()
	candidates, err := s.store.ListProxyQualityGuardCandidates(ctx)
	if err != nil {
		return nil, err
	}
	states, err := s.store.ListProxyQualityGuardStates(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(candidates))
	for _, candidate := range candidates {
		ids = append(ids, candidate.Proxy.ID)
	}
	health := map[int64]*ProxyLatencyInfo{}
	if s.latencyCache != nil && len(ids) > 0 {
		if loaded, err := s.latencyCache.GetProxyLatencies(ctx, ids); err == nil {
			health = loaded
		}
	}
	out := &ProxyQualityGuardOverview{Settings: cfg, Items: make([]ProxyQualityGuardItem, 0, len(candidates))}
	if s.lastRun != nil {
		last := *s.lastRun
		out.LastRun = &last
	}
	for _, candidate := range candidates {
		item := buildProxyQualityGuardItem(candidate, states[candidate.Proxy.ID], health[candidate.Proxy.ID], cfg)
		out.Summary.Total++
		switch {
		case item.State == ProxyQualityGuardStateDisabled:
			out.Summary.Disabled++
		case item.ConsecutiveFailures > 0:
			out.Summary.Failing++
		case item.PreUseReady && item.ProxyStatus == StatusActive:
			out.Summary.Healthy++
		default:
			out.Summary.Pending++
		}
		out.Items = append(out.Items, item)
	}
	return out, nil
}

func buildProxyQualityGuardItem(candidate ProxyQualityGuardCandidate, state *ProxyQualityGuardState, info *ProxyLatencyInfo, cfg ProxyQualityGuardSettings) ProxyQualityGuardItem {
	proxy := candidate.Proxy
	item := ProxyQualityGuardItem{
		ProxyID: proxy.ID, Name: proxy.Name, Protocol: proxy.Protocol, Host: proxy.Host, Port: proxy.Port,
		GroupName: proxy.GroupName, ProxyStatus: proxy.Status, State: ProxyQualityGuardStateActive,
		FixedBoundCount: candidate.FixedBoundCount, DynamicBoundCount: candidate.DynamicBoundCount,
		Managed: candidate.FixedBoundCount == 0 || cfg.IncludeFixedBound,
	}
	if state != nil {
		item.State = state.State
		item.Rounds = state.Rounds
		item.ConsecutiveFailures = state.ConsecutiveFailures
		item.DisabledUntil = state.DisabledUntil
		item.LastCheckedAt = state.LastCheckedAt
		item.LastSuccessAt = state.LastSuccessAt
		item.LastError = state.LastError
	}
	identityProxy := proxy
	if item.State == ProxyQualityGuardStateDisabled {
		// 巡检禁用期间的检测按启用态身份写入缓存。
		identityProxy.Status = StatusActive
	}
	if ProxyLatencyMatchesProxy(info, &identityProxy) {
		item.QualityScore = info.QualityScore
		item.QualityGrade = info.QualityGrade
		item.QualityStatus = info.QualityStatus
		item.LatencyMs = info.LatencyMs
		item.IPAddress = info.IPAddress
		item.CountryCode = info.CountryCode
		item.PreUseReady = cfg.PassesPreUseCheck(&identityProxy, info)
	}
	return item
}
