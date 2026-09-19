package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

type spendGuardSettingStub struct {
	SettingRepository
	mu  sync.Mutex
	raw string
}

func (r *spendGuardSettingStub) GetValue(context.Context, string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.raw, nil
}

func (r *spendGuardSettingStub) Set(_ context.Context, _, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.raw = value
	return nil
}

type spendGuardStoreStub struct {
	SpendGuardRepository
	calls      atomic.Int64
	window     int
	items      []SpendGuardOffender
	events     []SpendGuardEvent
	freezeErr  error
	freezeCall int
	changed    bool
}

func (r *spendGuardStoreStub) ListSpendGuardOffenders(_ context.Context, window int) ([]SpendGuardOffender, error) {
	r.calls.Add(1)
	r.window = window
	return r.items, nil
}

func (r *spendGuardStoreStub) ListSpendGuardEvents(context.Context, int) ([]SpendGuardEvent, error) {
	return r.events, nil
}

func (r *spendGuardStoreStub) FreezeAPIKeyForSpendGuard(context.Context, SpendGuardFreezeInput) (string, bool, error) {
	r.freezeCall++
	return "test-key", r.changed, r.freezeErr
}

func (r *spendGuardStoreStub) UnfreezeAPIKeyForSpendGuard(context.Context, int64) (string, bool, error) {
	return "test-key", r.changed, r.freezeErr
}

type spendGuardInvalidatorStub struct{ keys []string }

func (s *spendGuardInvalidatorStub) InvalidateAuthCacheByKey(_ context.Context, key string) {
	s.keys = append(s.keys, key)
}

func spendGuardTestSettings(t *testing.T, cfg SpendGuardSettings) *spendGuardSettingStub {
	t.Helper()
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	return &spendGuardSettingStub{raw: string(raw)}
}

func TestSpendGuardSettingsHotReloadResetsRunningTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cfg := DefaultSpendGuardSettings()
		cfg.Enabled = true
		store := &spendGuardStoreStub{}
		svc := NewSpendGuardService(store, &spendGuardInvalidatorStub{}, spendGuardTestSettings(t, cfg))
		svc.Start()
		defer svc.Stop()
		synctest.Wait()
		require.Equal(t, int64(1), store.calls.Load())
		time.Sleep(5 * time.Second)
		cfg.IntervalSeconds = 10
		_, err := svc.UpdateSettings(context.Background(), cfg)
		require.NoError(t, err)
		synctest.Wait()
		time.Sleep(9 * time.Second)
		synctest.Wait()
		require.Equal(t, int64(1), store.calls.Load())
		time.Sleep(time.Second)
		synctest.Wait()
		require.Equal(t, int64(2), store.calls.Load(), "new interval must take effect without restart")
	})
}

func TestSpendGuardConfiguredWindowAndStateSurviveServiceRestart(t *testing.T) {
	cfg := DefaultSpendGuardSettings()
	cfg.WindowMinutes = 30
	settings := spendGuardTestSettings(t, cfg)
	store := &spendGuardStoreStub{items: []SpendGuardOffender{{APIKeyID: 4, Frozen: true, Status: StatusAPIKeyDisabled}},
		events: []SpendGuardEvent{{APIKeyID: 4, Action: "frozen"}}}
	for range 2 {
		svc := NewSpendGuardService(store, nil, settings)
		items, err := svc.Offenders(context.Background(), 0)
		require.NoError(t, err)
		require.Equal(t, 30, store.window)
		require.Len(t, items, 1)
		require.True(t, items[0].Frozen)
		require.Zero(t, items[0].Requests, "frozen keys remain visible without recent requests")
		events, err := svc.Events(context.Background())
		require.NoError(t, err)
		require.Len(t, events, 1)
	}
}

func TestSpendGuardInvalidatesOnlyAfterSuccessfulStateChange(t *testing.T) {
	for _, tc := range []struct {
		name    string
		changed bool
		err     error
		want    int
	}{{"committed", true, nil, 1}, {"no-op", false, nil, 0}, {"rollback", false, errors.New("database failure"), 0}} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultSpendGuardSettings()
			cfg.Enabled, cfg.MinRequests, cfg.TokensPerMinute = true, 1, 10
			store := &spendGuardStoreStub{changed: tc.changed, freezeErr: tc.err,
				items: []SpendGuardOffender{{APIKeyID: 1, Status: StatusAPIKeyActive, Requests: 1, TokensPerMin: 11}}}
			invalidator := &spendGuardInvalidatorStub{}
			svc := NewSpendGuardService(store, invalidator, spendGuardTestSettings(t, cfg))
			svc.runOnce()
			require.Len(t, invalidator.keys, tc.want)
			require.Equal(t, 1, store.freezeCall)
			invalidator.keys = nil
			err := svc.Unfreeze(context.Background(), 1)
			require.ErrorIs(t, err, tc.err)
			require.Len(t, invalidator.keys, tc.want)
		})
	}
}

func TestSpendGuardBreachRequiresSampleAndHonorsTokenOptOut(t *testing.T) {
	cfg := DefaultSpendGuardSettings()
	cfg.MinRequests, cfg.TokensPerMinute = 5, 1000
	breach, _ := spendGuardBreach(SpendGuardOffender{Requests: 4, TokensPerMin: 1001}, cfg)
	require.False(t, breach)
	breach, reason := spendGuardBreach(SpendGuardOffender{Requests: 5, TokensPerMin: 1000}, cfg)
	require.True(t, breach)
	require.Equal(t, "token velocity", reason)
	cfg.TokensPerMinute = 0
	breach, _ = spendGuardBreach(SpendGuardOffender{Requests: 5, TokensPerMin: 1000000}, cfg)
	require.False(t, breach)
	breach, reason = spendGuardBreach(SpendGuardOffender{Requests: 1, Errors: 4, ErrRate: 0.8}, cfg)
	require.True(t, breach)
	require.Equal(t, "error rate", reason)
}
