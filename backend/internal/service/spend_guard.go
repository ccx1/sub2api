package service

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"sync"
	"time"
)

type SpendGuardOffender struct {
	APIKeyID     int64      `json:"api_key_id"`
	Name         string     `json:"name"`
	UserID       int64      `json:"user_id"`
	Status       string     `json:"status"`
	Requests     int64      `json:"requests"`
	Tokens       int64      `json:"tokens"`
	TokensPerMin float64    `json:"tokens_per_min"`
	Errors       int64      `json:"errors"`
	ErrRate      float64    `json:"err_rate"`
	Frozen       bool       `json:"frozen"`
	ReleasedAt   *time.Time `json:"-"`
}

type SpendGuardEvent struct {
	At       time.Time `json:"at"`
	APIKeyID int64     `json:"api_key_id"`
	Name     string    `json:"name"`
	Action   string    `json:"action"`
	Reason   string    `json:"reason"`
}

type SpendGuardFreezeInput struct {
	APIKeyID   int64
	Reason     string
	ReleasedAt *time.Time
}

// SpendGuardRepository 与 API Key 状态在同一事务内保存冻结和事件。
type SpendGuardRepository interface {
	ListSpendGuardOffenders(context.Context, int) ([]SpendGuardOffender, error)
	ListSpendGuardEvents(context.Context, int) ([]SpendGuardEvent, error)
	FreezeAPIKeyForSpendGuard(context.Context, SpendGuardFreezeInput) (string, bool, error)
	UnfreezeAPIKeyForSpendGuard(context.Context, int64) (string, bool, error)
}

type SpendGuardKeyStore interface {
	InvalidateAuthCacheByKey(context.Context, string)
}

type SpendGuardService struct {
	store    SpendGuardRepository
	keys     SpendGuardKeyStore
	settings SettingRepository

	stopCh    chan struct{}
	wakeCh    chan struct{}
	startOnce sync.Once
	stopOnce  sync.Once
	wg        sync.WaitGroup
}

func NewSpendGuardService(store SpendGuardRepository, keys SpendGuardKeyStore, settings SettingRepository) *SpendGuardService {
	return &SpendGuardService{store: store, keys: keys, settings: settings,
		stopCh: make(chan struct{}), wakeCh: make(chan struct{}, 1)}
}

// 保留现有 Wire provider 签名；生产 API Key 仓储同时实现冻结事务。
func ProvideSpendGuardService(_ *sql.DB, apiKeyService *APIKeyService, settings SettingRepository) *SpendGuardService {
	store, _ := apiKeyService.apiKeyRepo.(SpendGuardRepository)
	svc := NewSpendGuardService(store, apiKeyService, settings)
	svc.Start()
	return svc
}

func (s *SpendGuardService) Start() {
	if s == nil || s.store == nil || s.keys == nil {
		return
	}
	s.startOnce.Do(func() {
		s.wg.Add(1)
		go s.run()
	})
}

func (s *SpendGuardService) run() {
	defer s.wg.Done()
	s.runOnce()
	timer := time.NewTimer(s.checkInterval())
	defer timer.Stop()
	for {
		select {
		case <-timer.C:
			s.runOnce()
		case <-s.wakeCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-s.stopCh:
			return
		}
		timer.Reset(s.checkInterval())
	}
}

func (s *SpendGuardService) checkInterval() time.Duration {
	return time.Duration(s.currentSettings().IntervalSeconds) * time.Second
}

func (s *SpendGuardService) Stop() {
	if s == nil {
		return
	}
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.wg.Wait()
}

func (s *SpendGuardService) Offenders(ctx context.Context, windowMinutes int) ([]SpendGuardOffender, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("spend guard repository unavailable")
	}
	if windowMinutes <= 0 {
		windowMinutes = s.currentSettings().WindowMinutes
	}
	return s.store.ListSpendGuardOffenders(ctx, windowMinutes)
}

func (s *SpendGuardService) Events(ctx context.Context) ([]SpendGuardEvent, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("spend guard repository unavailable")
	}
	return s.store.ListSpendGuardEvents(ctx, 100)
}

func spendGuardBreach(item SpendGuardOffender, cfg SpendGuardSettings) (bool, string) {
	if item.Requests+item.Errors < int64(cfg.MinRequests) {
		return false, ""
	}
	if cfg.TokensPerMinute > 0 && item.TokensPerMin >= cfg.TokensPerMinute {
		return true, "token velocity"
	}
	if item.Errors > 0 && item.ErrRate >= cfg.MaxErrorRate {
		return true, "error rate"
	}
	return false, ""
}

func (s *SpendGuardService) runOnce() {
	cfg := s.currentSettings()
	if !cfg.Enabled {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	offenders, err := s.Offenders(ctx, cfg.WindowMinutes)
	if err != nil {
		slog.Warn("spend_guard.offenders_failed", "error", err)
		return
	}
	for _, item := range offenders {
		breach, reason := spendGuardBreach(item, cfg)
		if !breach || item.Frozen || item.Status != StatusAPIKeyActive {
			continue
		}
		key, changed, err := s.store.FreezeAPIKeyForSpendGuard(ctx, SpendGuardFreezeInput{
			APIKeyID: item.APIKeyID, Reason: reason, ReleasedAt: item.ReleasedAt,
		})
		if err != nil {
			slog.Warn("spend_guard.freeze_failed", "api_key_id", item.APIKeyID, "error", err)
			continue
		}
		if changed {
			s.invalidateKey(key)
			slog.Warn("spend_guard.key_frozen", "api_key_id", item.APIKeyID, "reason", reason)
		}
	}
}

func (s *SpendGuardService) Unfreeze(ctx context.Context, id int64) error {
	if s == nil || s.store == nil {
		return errors.New("spend guard repository unavailable")
	}
	key, changed, err := s.store.UnfreezeAPIKeyForSpendGuard(ctx, id)
	if err != nil {
		return err
	}
	if changed {
		s.invalidateKey(key)
	}
	return nil
}

func (s *SpendGuardService) invalidateKey(key string) {
	if s.keys == nil || key == "" {
		return
	}
	// 数据库提交后独立失效；状态更新触发的 outbox 保证失败后仍可重试。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.keys.InvalidateAuthCacheByKey(ctx, key)
}
