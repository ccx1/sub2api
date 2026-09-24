package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type codexSnapshotWriteRepo struct {
	AccountRepository
	mu           sync.Mutex
	extra        map[string]any
	oldStarted   chan struct{}
	releaseOld   chan struct{}
	releaseOnce  sync.Once
	oldWriteDone chan struct{}
	failOld      bool
}

func (r *codexSnapshotWriteRepo) UpdateExtra(ctx context.Context, _ int64, updates map[string]any) error {
	if used, _ := updates["codex_5h_used_percent"].(float64); used == 99 {
		close(r.oldStarted)
		defer close(r.oldWriteDone)
		select {
		case <-r.releaseOld:
		case <-ctx.Done():
			return ctx.Err()
		}
		if r.failOld {
			return errors.New("old snapshot write failed")
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, value := range updates {
		r.extra[key] = value
	}
	return nil
}

func (r *codexSnapshotWriteRepo) snapshot() map[string]any {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make(map[string]any, len(r.extra))
	for key, value := range r.extra {
		result[key] = value
	}
	return result
}

func (r *codexSnapshotWriteRepo) release() {
	r.releaseOnce.Do(func() { close(r.releaseOld) })
}

func newCodexSnapshotWriteRepo(t *testing.T) *codexSnapshotWriteRepo {
	t.Helper()
	r := &codexSnapshotWriteRepo{
		extra:        map[string]any{"codex_7d_used_percent": 42.0},
		oldStarted:   make(chan struct{}),
		releaseOld:   make(chan struct{}),
		oldWriteDone: make(chan struct{}),
	}
	t.Cleanup(r.release)
	return r
}

// 使用各入口的真实保存方法，仓库阻塞仅控制旧快照何时落库。
func codexSnapshotWriter(repo AccountRepository, name string, accountID int64) func(float64) error {
	gateway := &OpenAIGatewayService{accountRepo: repo, codexSnapshotThrottle: newAccountWriteThrottle(0)}
	usageService := &AccountUsageService{accountRepo: repo}
	rateLimit := &RateLimitService{accountRepo: repo}
	quota := &OpenAIQuotaService{accountRepo: repo}
	autoReset := &OpenAIQuotaAutoResetService{accountRepo: repo, quota: quota}
	account := &Account{ID: accountID, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	return func(used float64) error {
		ctx := context.Background()
		headers := make(http.Header)
		headers.Set("x-codex-primary-used-percent", strconv.FormatFloat(used, 'f', -1, 64))
		headers.Set("x-codex-primary-window-minutes", "300")
		headers.Set("x-codex-primary-reset-after-seconds", "600")
		updates := buildCodexUsageExtraUpdates(ParseCodexRateLimitHeaders(headers), time.Now())
		usage := &OpenAIQuotaUsage{
			RateLimit: &OpenAIRateLimit{PrimaryWindow: &OpenAIRateLimitWindow{
				UsedPercent: used, LimitWindowSeconds: 18000, ResetAfterSeconds: 600,
			}},
			RateLimitResetCredits: &OpenAIRateLimitResetCredits{},
		}
		switch name {
		case "gateway":
			gateway.UpdateCodexUsageSnapshotFromHeaders(ctx, accountID, headers)
		case "probe":
			usageService.persistOpenAICodexProbeSnapshot(accountID, updates)
		case "ratelimit":
			rateLimit.persistOpenAICodexSnapshot(ctx, account, headers)
		case "shared":
			shared := *account
			shared.Extra = map[string]any{SharedPoolOwnerKey: int64(1)}
			return usageService.persistOpenAIUsageProbeSnapshot(ctx, &shared, updates)
		case "post_reset":
			return quota.CachePostResetSnapshot(ctx, accountID, usage)
		case "auto_reset":
			return autoReset.persistFreshUsage(ctx, accountID, usage, time.Now())
		default:
			return errors.New("unknown snapshot writer")
		}
		return nil
	}
}

func TestCodexSnapshotWritesSerializeAcrossSources(t *testing.T) {
	for _, tt := range []struct {
		oldWriter string
		newWriter string
		failOld   bool
	}{
		{oldWriter: "gateway", newWriter: "gateway"},
		{oldWriter: "gateway", newWriter: "probe"},
		{oldWriter: "gateway", newWriter: "ratelimit"},
		{oldWriter: "gateway", newWriter: "shared"},
		{oldWriter: "gateway", newWriter: "post_reset"},
		{oldWriter: "gateway", newWriter: "auto_reset"},
		{oldWriter: "probe", newWriter: "gateway"},
		{oldWriter: "ratelimit", newWriter: "probe"},
		{oldWriter: "gateway", newWriter: "probe", failOld: true},
		{oldWriter: "probe", newWriter: "gateway", failOld: true},
		{oldWriter: "ratelimit", newWriter: "gateway", failOld: true},
	} {
		name := tt.oldWriter + "_then_" + tt.newWriter
		if tt.failOld {
			name += "_after_failure"
		}
		t.Run(name, func(t *testing.T) {
			repo := newCodexSnapshotWriteRepo(t)
			repo.failOld = tt.failOld
			oldWrite := codexSnapshotWriter(repo, tt.oldWriter, 9083)
			newWrite := codexSnapshotWriter(repo, tt.newWriter, 9083)
			oldDone := make(chan error, 1)
			go func() { oldDone <- oldWrite(99) }()
			select {
			case <-repo.oldStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("old snapshot did not enter repository")
			}
			newStarted := make(chan struct{})
			newDone := make(chan error, 1)
			go func() {
				close(newStarted)
				newDone <- newWrite(100)
			}()
			<-newStarted
			select {
			case err := <-newDone:
				t.Fatalf("100%% snapshot returned before the pending 99%% write finished: %v", err)
			case <-time.After(30 * time.Millisecond):
			}
			repo.release()
			select {
			case err := <-newDone:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("100% snapshot did not finish after the old write was released")
			}
			select {
			case err := <-oldDone:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("old snapshot writer did not return")
			}
			extra := repo.snapshot()
			require.Equal(t, 100.0, extra["codex_5h_used_percent"])
			require.Equal(t, 42.0, extra["codex_7d_used_percent"], "a partial 5h update must preserve the 7d snapshot")
			account := &Account{Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Extra: extra}
			require.False(t, account.IsSchedulable(), "the exhausted snapshot must be visible when its writer returns")
		})
	}
}

func TestCodexSnapshotNormalWritesRemainAsync(t *testing.T) {
	for _, source := range []string{"gateway", "probe"} {
		t.Run(source, func(t *testing.T) {
			repo := newCodexSnapshotWriteRepo(t)
			write := codexSnapshotWriter(repo, source, 9084)
			done := make(chan error, 1)
			go func() { done <- write(99) }()
			select {
			case <-repo.oldStarted:
			case <-time.After(2 * time.Second):
				t.Fatal("normal snapshot did not start writing")
			}
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(2 * time.Second):
				t.Fatal("non-exhausted snapshot unexpectedly waits for persistence")
			}
			repo.release()
			select {
			case <-repo.oldWriteDone:
			case <-time.After(2 * time.Second):
				t.Fatal("normal snapshot did not finish writing")
			}
		})
	}
}
