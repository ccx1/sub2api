package service

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"sync"
	"time"
)

var (
	ErrAccountTokenGuardBusy             = errors.New("该账号正在巡检或重登")
	ErrAccountTokenGuardStale            = errors.New("账号或巡检状态已更新，已丢弃旧结果")
	ErrAccountTokenGuardStoreUnavailable = errors.New("账号仓储缺少凭证守护专用能力")
)

// AccountTokenGuardAccountStore 不放宽通用账号列表和管理员写接口。
type AccountTokenGuardAccountStore interface {
	ListTokenGuardCandidates(context.Context, []int64) ([]Account, error)
	ApplyTokenGuardRepair(context.Context, *Account, map[string]any) (time.Time, error)
}

type AccountTokenGuardVersionedStore interface {
	UpsertStateIfUnchanged(context.Context, AccountTokenGuardState) (time.Time, error)
}

func (s *AccountTokenGuardService) guardAccountStore() (AccountTokenGuardAccountStore, error) {
	if _, ok := s.repo.(AccountTokenGuardVersionedStore); !ok {
		return nil, ErrAccountTokenGuardStoreUnavailable
	}
	store, ok := s.accounts.(AccountTokenGuardAccountStore)
	if !ok {
		return nil, ErrAccountTokenGuardStoreUnavailable
	}
	return store, nil
}

func guardAccountEligible(account *Account) bool {
	return account != nil && account.Platform == PlatformOpenAI && account.IsOAuth() && !account.IsShadow() &&
		(account.Status == StatusActive || account.Status == StatusError) &&
		(!account.AutoPauseOnExpired || account.ExpiresAt == nil || account.ExpiresAt.After(time.Now()))
}

func guardAccountSnapshot(account *Account) *Account {
	if account == nil {
		return nil
	}
	copy := *account
	encoded, err := json.Marshal(account.Credentials)
	if err != nil {
		return nil
	}
	copy.Credentials = nil
	if err := json.Unmarshal(encoded, &copy.Credentials); err != nil {
		return nil
	}
	copy.Extra = maps.Clone(account.Extra)
	if account.ProxyID != nil {
		id := *account.ProxyID
		copy.ProxyID = &id
	}
	return &copy
}

// 锁跨越取快照、外部请求和状态落库；不同账号不互相阻塞。
func (s *AccountTokenGuardService) beginGuardAccount(ctx context.Context, id int64) (context.Context, func(), error) {
	s.lifecycle.Lock()
	defer s.lifecycle.Unlock()
	if s.stopped {
		return nil, nil, errors.New("凭证守护服务已停止")
	}
	accountCtx, cancel := context.WithCancel(ctx)
	if _, busy := s.accountRuns.LoadOrStore(id, cancel); busy {
		cancel()
		return nil, nil, ErrAccountTokenGuardBusy
	}
	s.wg.Add(1)
	var once sync.Once
	finish := func() { once.Do(func() { cancel(); s.accountRuns.Delete(id); s.wg.Done() }) }
	return accountCtx, finish, nil
}

func (s *AccountTokenGuardService) storeGuardState(ctx context.Context, state *AccountTokenGuardState) error {
	store, ok := s.repo.(AccountTokenGuardVersionedStore)
	if !ok {
		return ErrAccountTokenGuardStoreUnavailable
	}
	version, err := store.UpsertStateIfUnchanged(ctx, *state)
	if err == nil {
		state.UpdatedAt = version
	}
	return err
}

func guardStateUsesAccount(state *AccountTokenGuardState, account *Account) {
	state.AccountVersion = guardAccountSnapshot(account)
	state.AccountID, state.AccountName = account.ID, account.Name
	state.AccountStatus, state.Schedulable = account.Status, account.Schedulable
}

func isGuardSuperseded(err error) bool {
	return errors.Is(err, ErrAccountTokenGuardStale) || errors.Is(err, ErrAccountTokenGuardBusy)
}

func (s *AccountTokenGuardService) storeGuardFailure(state *AccountTokenGuardState, stats *AccountTokenGuardStats) bool {
	ctx, cancel := newGuardStoreContext()
	defer cancel()
	if err := s.storeGuardState(ctx, state); err != nil {
		if !isGuardSuperseded(err) {
			stats.PersistenceErrors++
			stats.Failed++
		}
		return false
	}
	stats.Failed++
	return true
}

func (s *AccountTokenGuardService) applyGuardRepair(ctx context.Context, account *Account, credentials map[string]any) error {
	if !guardAccountEligible(account) {
		return ErrAccountTokenGuardStale
	}
	store, err := s.guardAccountStore()
	if err != nil {
		return err
	}
	version, err := store.ApplyTokenGuardRepair(ctx, account, credentials)
	if err != nil {
		return err
	}
	account.UpdatedAt, account.Status, account.ErrorMessage = version, StatusActive, ""
	if credentials != nil {
		account.Credentials = credentials
	}
	if s.invalidator != nil {
		cacheCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		_ = s.invalidator.InvalidateToken(cacheCtx, account)
	}
	return nil
}
