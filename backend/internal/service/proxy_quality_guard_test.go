package service

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type proxyQualityGuardStoreStub struct {
	mu         sync.Mutex
	candidates []ProxyQualityGuardCandidate
	states     map[int64]*ProxyQualityGuardState
	events     []ProxyQualityGuardEvent
	runtime    map[int64]int64
	deleteErr  error
	deleted    []int64
}

func newProxyQualityGuardStoreStub(proxies ...Proxy) *proxyQualityGuardStoreStub {
	store := &proxyQualityGuardStoreStub{states: map[int64]*ProxyQualityGuardState{}, runtime: map[int64]int64{}}
	for _, proxy := range proxies {
		store.candidates = append(store.candidates, ProxyQualityGuardCandidate{Proxy: proxy})
	}
	return store
}

func (s *proxyQualityGuardStoreStub) ListProxyQualityGuardCandidates(context.Context) ([]ProxyQualityGuardCandidate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ProxyQualityGuardCandidate(nil), s.candidates...), nil
}

func (s *proxyQualityGuardStoreStub) ListProxyQualityGuardStates(context.Context) (map[int64]*ProxyQualityGuardState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[int64]*ProxyQualityGuardState, len(s.states))
	for id, state := range s.states {
		copied := *state
		out[id] = &copied
	}
	return out, nil
}

func (s *proxyQualityGuardStoreStub) save(state *ProxyQualityGuardState) {
	copied := *state
	s.states[state.ProxyID] = &copied
}

func (s *proxyQualityGuardStoreStub) SaveProxyQualityGuardState(_ context.Context, state *ProxyQualityGuardState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.save(state)
	return nil
}

func (s *proxyQualityGuardStoreStub) setStatus(proxyID int64, from, to string) bool {
	for i := range s.candidates {
		if s.candidates[i].Proxy.ID == proxyID && s.candidates[i].Proxy.Status == from {
			s.candidates[i].Proxy.Status = to
			return true
		}
	}
	return false
}

func (s *proxyQualityGuardStoreStub) DisableProxyForQualityGuard(_ context.Context, state *ProxyQualityGuardState, event ProxyQualityGuardEvent) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.save(state)
	s.events = append(s.events, event)
	return s.setStatus(state.ProxyID, StatusActive, "inactive"), nil
}

func (s *proxyQualityGuardStoreStub) RestoreProxyForQualityGuard(_ context.Context, state *ProxyQualityGuardState, event ProxyQualityGuardEvent) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.save(state)
	changed := s.setStatus(state.ProxyID, "inactive", StatusActive)
	if changed {
		s.events = append(s.events, event)
	}
	return changed, nil
}

func (s *proxyQualityGuardStoreStub) DeleteProxyForQualityGuard(_ context.Context, state *ProxyQualityGuardState, event ProxyQualityGuardEvent) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleteErr != nil {
		return false, s.deleteErr
	}
	s.save(state)
	s.events = append(s.events, event)
	for i := range s.candidates {
		if s.candidates[i].Proxy.ID == state.ProxyID {
			s.candidates = append(s.candidates[:i], s.candidates[i+1:]...)
			s.deleted = append(s.deleted, state.ProxyID)
			return true, nil
		}
	}
	return false, nil
}

func (s *proxyQualityGuardStoreStub) ResetProxyQualityGuardState(_ context.Context, proxyID int64, event ProxyQualityGuardEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if state := s.states[proxyID]; state != nil && state.State == ProxyQualityGuardStateDisabled {
		s.setStatus(proxyID, "inactive", StatusActive)
	}
	delete(s.states, proxyID)
	s.events = append(s.events, event)
	return nil
}

func (s *proxyQualityGuardStoreStub) AppendProxyQualityGuardEvent(_ context.Context, event ProxyQualityGuardEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

func (s *proxyQualityGuardStoreStub) ListProxyQualityGuardEvents(context.Context, int) ([]ProxyQualityGuardEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ProxyQualityGuardEvent(nil), s.events...), nil
}

func (s *proxyQualityGuardStoreStub) CountProxyQualityGuardRuntimeFailures(_ context.Context, ids []int64) (map[int64]int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int64]int64{}
	for _, id := range ids {
		out[id] = s.runtime[id]
	}
	return out, nil
}

func (s *proxyQualityGuardStoreStub) ClearProxyQualityGuardRuntimeFailures(_ context.Context, proxyID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runtime, proxyID)
	return nil
}

func (s *proxyQualityGuardStoreStub) status(proxyID int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, candidate := range s.candidates {
		if candidate.Proxy.ID == proxyID {
			return candidate.Proxy.Status
		}
	}
	return ""
}

func (s *proxyQualityGuardStoreStub) actions() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.events))
	for _, event := range s.events {
		out = append(out, event.Action)
	}
	return out
}

type proxyQualityGuardClock struct{ now time.Time }

func (c *proxyQualityGuardClock) Now() time.Time { return c.now }

func newProxyQualityGuardTestService(store *proxyQualityGuardStoreStub, clock *proxyQualityGuardClock, pass *bool) *ProxyQualityGuardService {
	svc := NewProxyQualityGuardService(store, nil, nil, nil, nil, nil)
	svc.now = clock.Now
	svc.check = func(context.Context, *Proxy, ProxyQualityGuardSettings) proxyQualityGuardCheck {
		if *pass {
			score := 90
			return proxyQualityGuardCheck{passed: true, score: &score, grade: "A"}
		}
		score := 20
		return proxyQualityGuardCheck{score: &score, grade: "F", reason: "质量分 20 低于阈值 60"}
	}
	return svc
}

func proxyQualityGuardTestSettings() ProxyQualityGuardSettings {
	cfg := DefaultProxyQualityGuardSettings()
	cfg.Enabled = true
	cfg.DisableMinutes = 5
	cfg.MaxRounds = 3
	cfg.CheckIntervalMinutes = 1
	return cfg
}

func TestProxyQualityGuardDisableRestoreThenDelete(t *testing.T) {
	store := newProxyQualityGuardStoreStub(Proxy{ID: 7, Name: "p7", Status: StatusActive})
	clock := &proxyQualityGuardClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	pass := false
	svc := newProxyQualityGuardTestService(store, clock, &pass)
	cfg := proxyQualityGuardTestSettings()
	ctx := context.Background()

	// 第 1 轮：检测失败立即禁用。
	res, err := svc.runCycle(ctx, cfg, false)
	require.NoError(t, err)
	require.Equal(t, 1, res.Disabled)
	require.Equal(t, "inactive", store.status(7))
	require.Equal(t, 1, store.states[7].Rounds)

	// 冷却未到期不复检。
	clock.now = clock.now.Add(4 * time.Minute)
	res, err = svc.runCycle(ctx, cfg, false)
	require.NoError(t, err)
	require.Zero(t, res.Checked)

	// 冷却到期复检通过：恢复启用，保留轮数。
	pass = true
	clock.now = clock.now.Add(2 * time.Minute)
	res, err = svc.runCycle(ctx, cfg, false)
	require.NoError(t, err)
	require.Equal(t, 1, res.Restored)
	require.Equal(t, StatusActive, store.status(7))
	require.Equal(t, 1, store.states[7].Rounds)

	// 第 2 轮：再次失败再次禁用。
	pass = false
	clock.now = clock.now.Add(2 * time.Minute)
	_, err = svc.runCycle(ctx, cfg, false)
	require.NoError(t, err)
	require.Equal(t, "inactive", store.status(7))
	require.Equal(t, 2, store.states[7].Rounds)

	// 冷却到期复检仍失败：达到第 3 轮，自动删除。
	clock.now = clock.now.Add(6 * time.Minute)
	res, err = svc.runCycle(ctx, cfg, false)
	require.NoError(t, err)
	require.Equal(t, 1, res.Deleted)
	require.Equal(t, []int64{7}, store.deleted)
	require.Equal(t, ProxyQualityGuardStateDeleted, store.states[7].State)
	require.Equal(t, []string{ProxyQualityGuardActionDisabled, ProxyQualityGuardActionRestored,
		ProxyQualityGuardActionDisabled, ProxyQualityGuardActionDeleted}, store.actions())
}

func TestProxyQualityGuardFailureThresholdAndFixedBinding(t *testing.T) {
	store := newProxyQualityGuardStoreStub(Proxy{ID: 1, Status: StatusActive}, Proxy{ID: 2, Status: StatusActive})
	store.candidates[1].FixedBoundCount = 2
	clock := &proxyQualityGuardClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	pass := false
	svc := newProxyQualityGuardTestService(store, clock, &pass)
	cfg := proxyQualityGuardTestSettings()
	cfg.FailureThreshold = 2
	ctx := context.Background()

	res, err := svc.runCycle(ctx, cfg, false)
	require.NoError(t, err)
	// 固定绑定代理默认不接管。
	require.Equal(t, 1, res.Checked)
	require.Equal(t, StatusActive, store.status(1))
	require.Equal(t, 1, store.states[1].ConsecutiveFailures)

	clock.now = clock.now.Add(time.Minute)
	res, err = svc.runCycle(ctx, cfg, false)
	require.NoError(t, err)
	require.Equal(t, 1, res.Disabled)
	require.Equal(t, "inactive", store.status(1))
	require.Equal(t, StatusActive, store.status(2))
}

func TestProxyQualityGuardSkipsDeleteWhenFixedBound(t *testing.T) {
	store := newProxyQualityGuardStoreStub(Proxy{ID: 3, Status: StatusActive})
	store.candidates[0].FixedBoundCount = 1
	clock := &proxyQualityGuardClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	pass := false
	svc := newProxyQualityGuardTestService(store, clock, &pass)
	cfg := proxyQualityGuardTestSettings()
	cfg.IncludeFixedBound = true
	cfg.MaxRounds = 1

	res, err := svc.runCycle(context.Background(), cfg, false)
	require.NoError(t, err)
	require.Zero(t, res.Deleted)
	require.Equal(t, "inactive", store.status(3))
	require.Equal(t, ProxyQualityGuardStateDisabled, store.states[3].State)
	require.Contains(t, store.actions(), ProxyQualityGuardActionDeleteSkip)
}

func TestProxyQualityGuardIgnoresManuallyDisabledProxy(t *testing.T) {
	store := newProxyQualityGuardStoreStub(Proxy{ID: 4, Status: "inactive"})
	clock := &proxyQualityGuardClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	pass := true
	svc := newProxyQualityGuardTestService(store, clock, &pass)

	res, err := svc.runCycle(context.Background(), proxyQualityGuardTestSettings(), true)
	require.NoError(t, err)
	require.Zero(t, res.Checked)
	require.Equal(t, "inactive", store.status(4))
}

func TestProxyQualityGuardRuntimeFailuresTriggerEarlyRecheck(t *testing.T) {
	store := newProxyQualityGuardStoreStub(Proxy{ID: 5, Status: StatusActive})
	clock := &proxyQualityGuardClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	pass := true
	svc := newProxyQualityGuardTestService(store, clock, &pass)
	cfg := proxyQualityGuardTestSettings()
	cfg.CheckIntervalMinutes = 60
	ctx := context.Background()

	_, err := svc.runCycle(ctx, cfg, false)
	require.NoError(t, err)
	clock.now = clock.now.Add(time.Minute)
	res, err := svc.runCycle(ctx, cfg, false)
	require.NoError(t, err)
	require.Zero(t, res.Checked)

	store.runtime[5] = int64(cfg.RuntimeFailureThreshold)
	pass = false
	res, err = svc.runCycle(ctx, cfg, false)
	require.NoError(t, err)
	require.Equal(t, 1, res.Checked)
	require.Equal(t, "inactive", store.status(5))
}

func TestProxyQualityGuardStableResetClearsRounds(t *testing.T) {
	store := newProxyQualityGuardStoreStub(Proxy{ID: 6, Status: StatusActive})
	failedAt := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	store.states[6] = &ProxyQualityGuardState{ProxyID: 6, State: ProxyQualityGuardStateActive, Rounds: 2, LastFailedAt: &failedAt, LastCheckedAt: &failedAt}
	clock := &proxyQualityGuardClock{now: failedAt.Add(25 * time.Hour)}
	pass := false
	svc := newProxyQualityGuardTestService(store, clock, &pass)

	_, err := svc.runCycle(context.Background(), proxyQualityGuardTestSettings(), false)
	require.NoError(t, err)
	// 稳定期满后轮数清零，本次失败记为第 1 轮而不是直接删除。
	require.Equal(t, 1, store.states[6].Rounds)
	require.Equal(t, "inactive", store.status(6))
	require.Empty(t, store.deleted)
}

func TestProxyQualityGuardSettingsNormalizeAndGate(t *testing.T) {
	cfg := normalizeProxyQualityGuardSettings(ProxyQualityGuardSettings{CheckMode: "FULL", PreUseCheck: "bogus", MaxRounds: 0, Concurrency: 99})
	def := DefaultProxyQualityGuardSettings()
	require.Equal(t, ProxyQualityGuardCheckModeFull, cfg.CheckMode)
	require.Equal(t, def.PreUseCheck, cfg.PreUseCheck)
	require.Equal(t, def.MaxRounds, cfg.MaxRounds)
	require.Equal(t, def.Concurrency, cfg.Concurrency)
	require.Equal(t, def, ParseProxyQualityGuardSettings("{bad"))

	proxy := &Proxy{ID: 1, Protocol: "http", Host: "1.1.1.1", Port: 80, Status: StatusActive}
	checkedAt := time.Now().Unix()
	score := 80
	good := &ProxyLatencyInfo{ProxyIdentity: ProxyProbeIdentity(proxy), Success: true, QualityCheckedAt: &checkedAt, QualityScore: &score, UpdatedAt: time.Now()}

	cfg = DefaultProxyQualityGuardSettings()
	degraded, valid := cfg.ApplyPoolGate(proxy, nil, false, true)
	require.False(t, degraded)
	require.True(t, valid, "巡检未开启时不影响原有选择")

	cfg.Enabled = true
	degraded, valid = cfg.ApplyPoolGate(proxy, nil, false, true)
	require.True(t, degraded, "prefer 模式下未检测代理降为后备")
	require.True(t, valid)
	degraded, valid = cfg.ApplyPoolGate(proxy, good, false, true)
	require.False(t, degraded)
	require.True(t, valid)

	cfg.PreUseCheck = ProxyQualityGuardPreUseStrict
	_, valid = cfg.ApplyPoolGate(proxy, nil, false, true)
	require.False(t, valid, "strict 模式下未检测代理被排除")
	lowScore := 30
	low := *good
	low.QualityScore = &lowScore
	_, valid = cfg.ApplyPoolGate(proxy, &low, false, true)
	require.False(t, valid)

	cfg.CheckMode = ProxyQualityGuardCheckModeBasic
	_, valid = cfg.ApplyPoolGate(proxy, &low, false, true)
	require.True(t, valid, "basic 模式只要求连通性检测成功")
}
