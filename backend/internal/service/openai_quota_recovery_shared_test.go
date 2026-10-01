package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type quotaRecoverySharedRepo struct {
	*quotaRecoveryTestRepo
	entered        chan struct{}
	release        chan struct{}
	recoveryLocked bool
}

func (r *quotaRecoverySharedRepo) ClearOpenAIRateLimitIfObserved(ctx context.Context, id int64, limited, reset time.Time) (bool, error) {
	mu := &openAICodexSnapshotWriteLocks[uint64(id)%uint64(len(openAICodexSnapshotWriteLocks))]
	r.recoveryLocked = !mu.TryLock()
	if !r.recoveryLocked {
		mu.Unlock()
	}
	return r.quotaRecoveryTestRepo.ClearOpenAIRateLimitIfObserved(ctx, id, limited, reset)
}

func (r *quotaRecoverySharedRepo) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	close(r.entered)
	select {
	case <-r.release:
		return r.quotaRecoveryTestRepo.UpdateExtra(ctx, id, updates)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestOpenAIQuotaRecoverySharedSnapshotPreservesLockAndGeneration(t *testing.T) {
	for _, mode := range []string{"recovered", "new 429", "write failed", "request canceled"} {
		t.Run(mode, func(t *testing.T) {
			base := &quotaRecoveryTestRepo{account: quotaRecoveryAccount()}
			account := base.account
			account.Extra = map[string]any{SharedPoolOwnerKey: int64(5)}
			observed := observeOpenAIQuotaRecovery(&account, time.Now())
			repo := &quotaRecoverySharedRepo{quotaRecoveryTestRepo: base, entered: make(chan struct{}), release: make(chan struct{})}
			var once sync.Once
			unblock := func() { once.Do(func() { close(repo.release) }) }
			t.Cleanup(unblock)
			if mode == "write failed" {
				base.writeErr = errors.New("snapshot unavailable")
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "request canceled" {
				cancel()
			}
			svc := &AccountUsageService{accountRepo: repo}
			done := make(chan error, 1)
			go func() { done <- svc.persistOpenAIUsageProbeSnapshot(ctx, &account, zeroQuotaUpdates(), observed) }()
			select {
			case <-repo.entered:
			case <-time.After(time.Second):
				t.Fatal("共享池快照没有开始写入")
			}
			mu := &openAICodexSnapshotWriteLocks[uint64(account.ID)%uint64(len(openAICodexSnapshotWriteLocks))]
			if mu.TryLock() {
				mu.Unlock()
				t.Fatal("共享池写入必须持有快照锁")
			}
			base.mu.Lock()
			clearsBeforePersist := base.clears
			if mode == "new 429" {
				newLimited := time.Now()
				base.account.RateLimitedAt = &newLimited
			}
			base.mu.Unlock()
			require.Zero(t, clearsBeforePersist, "落库完成前不能解除限流")
			unblock()
			select {
			case err := <-done:
				if mode == "write failed" {
					require.ErrorIs(t, err, base.writeErr)
				} else {
					require.NoError(t, err)
				}
			case <-time.After(time.Second):
				t.Fatal("共享池快照写入未完成")
			}
			current, err := base.GetByID(context.Background(), account.ID)
			require.NoError(t, err)
			require.Equal(t, mode == "recovered" || mode == "request canceled", current.RateLimitResetAt == nil)
			if mode == "write failed" {
				require.Zero(t, base.clears)
			} else {
				require.True(t, repo.recoveryLocked, "条件恢复必须和快照写入处于同一个锁范围")
			}
		})
	}
}
