package service

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type autoTransferRepoFake struct {
	get  func(context.Context, int64) (*SharedPoolAutoTransferSettings, error)
	save func(context.Context, int64, SharedPoolAutoTransferUpdate) (*SharedPoolAutoTransferSettings, error)
	due  func(context.Context, time.Time, int64, int) ([]int64, error)
	run  func(context.Context, int64, time.Time) (*SharedPoolEarningsTransfer, error)
}

func (r *autoTransferRepoFake) Get(ctx context.Context, id int64) (*SharedPoolAutoTransferSettings, error) {
	return r.get(ctx, id)
}
func (r *autoTransferRepoFake) Save(ctx context.Context, id int64, in SharedPoolAutoTransferUpdate) (*SharedPoolAutoTransferSettings, error) {
	return r.save(ctx, id, in)
}
func (r *autoTransferRepoFake) DueUserIDs(ctx context.Context, now time.Time, after int64, limit int) ([]int64, error) {
	return r.due(ctx, now, after, limit)
}
func (r *autoTransferRepoFake) RunDue(ctx context.Context, id int64, now time.Time) (*SharedPoolEarningsTransfer, error) {
	return r.run(ctx, id, now)
}

type autoTransferCacheFake struct {
	auth, balance []int64
	contextErrors []error
	withDeadlines []bool
	balanceErr    error
}

func (*autoTransferCacheFake) InvalidateAuthCacheByKey(context.Context, string)    {}
func (*autoTransferCacheFake) InvalidateAuthCacheByGroupID(context.Context, int64) {}
func (c *autoTransferCacheFake) InvalidateAuthCacheByUserID(ctx context.Context, id int64) {
	c.auth = append(c.auth, id)
	c.recordContext(ctx)
}
func (c *autoTransferCacheFake) InvalidateUserBalance(ctx context.Context, id int64) error {
	c.balance = append(c.balance, id)
	c.recordContext(ctx)
	return c.balanceErr
}

func (c *autoTransferCacheFake) recordContext(ctx context.Context) {
	c.contextErrors = append(c.contextErrors, ctx.Err())
	_, ok := ctx.Deadline()
	c.withDeadlines = append(c.withDeadlines, ok)
}

type autoTransferLeaderFake struct {
	acquired         bool
	acquire, release int
	key, owner       string
}

func (l *autoTransferLeaderFake) TryAcquireLeaderLock(_ context.Context, key, owner string, _ time.Duration) (bool, error) {
	l.acquire++
	l.key, l.owner = key, owner
	return l.acquired, nil
}
func (l *autoTransferLeaderFake) ReleaseLeaderLock(_ context.Context, key, owner string) error {
	if l.key == key && l.owner == owner {
		l.release++
	}
	return nil
}

func TestSharedPoolAutoTransferValidation(t *testing.T) {
	for _, threshold := range []float64{0.00000001, 0.12345678, 1, 1000000000} {
		require.NoError(t, ValidateSharedPoolAutoTransfer(SharedPoolAutoTransferUpdate{Threshold: threshold, DailyTime: "23:59"}))
	}
	for _, threshold := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), 0, -1, 0.000000001, 1.123456789, 1000000000.1} {
		require.ErrorIs(t, ValidateSharedPoolAutoTransfer(SharedPoolAutoTransferUpdate{Threshold: threshold, DailyTime: "00:00"}), ErrSharedPoolAutoTransferInvalid)
	}
	for _, clock := range []string{"", "1:00", "01:0", "24:00", "12:60", "01:00:00", " 01:00", "01:00 ", "-1:00"} {
		t.Run(clock, func(t *testing.T) {
			require.ErrorIs(t, ValidateSharedPoolAutoTransfer(SharedPoolAutoTransferUpdate{Enabled: true, Threshold: 1, DailyTime: clock}), ErrSharedPoolAutoTransferInvalid)
		})
	}
	require.NoError(t, ValidateSharedPoolAutoTransfer(SharedPoolAutoTransferUpdate{Enabled: true, Threshold: 1, DailyTime: "00:00"}))
}

func TestSharedPoolAutoTransferGetSaveBindUserAndContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	want := &SharedPoolAutoTransferSettings{Enabled: true, Threshold: 2.25, DailyTime: "18:30", Timezone: "Asia/Shanghai"}
	input := SharedPoolAutoTransferUpdate{Enabled: true, Threshold: 2.25, DailyTime: "18:30"}
	repo := &autoTransferRepoFake{
		get: func(got context.Context, id int64) (*SharedPoolAutoTransferSettings, error) {
			require.Same(t, ctx, got)
			require.Equal(t, int64(71), id)
			return want, nil
		},
		save: func(got context.Context, id int64, value SharedPoolAutoTransferUpdate) (*SharedPoolAutoTransferSettings, error) {
			require.Same(t, ctx, got)
			require.Equal(t, int64(71), id)
			require.Equal(t, input, value)
			return want, nil
		},
	}
	svc := NewSharedPoolAutoTransferService(repo, nil, nil, nil, nil)
	t.Cleanup(svc.Stop)
	settings, err := svc.Get(ctx, 71)
	require.NoError(t, err)
	require.Same(t, want, settings)
	settings, err = svc.Save(ctx, 71, input)
	require.NoError(t, err)
	require.Same(t, want, settings)
	upstream := errors.New("settings unavailable")
	repo.get = func(context.Context, int64) (*SharedPoolAutoTransferSettings, error) { return nil, upstream }
	_, err = svc.Get(ctx, 71)
	require.ErrorIs(t, err, upstream)
	repo.save = func(context.Context, int64, SharedPoolAutoTransferUpdate) (*SharedPoolAutoTransferSettings, error) {
		return nil, upstream
	}
	_, err = svc.Save(ctx, 71, input)
	require.ErrorIs(t, err, upstream)
}

func TestSharedPoolAutoTransferInvalidInputDoesNotReachRepository(t *testing.T) {
	svc := NewSharedPoolAutoTransferService(&autoTransferRepoFake{}, nil, nil, nil, nil)
	t.Cleanup(svc.Stop)
	for _, id := range []int64{0, -1} {
		_, err := svc.Get(context.Background(), id)
		require.ErrorIs(t, err, ErrSharedPoolAutoTransferInvalid)
		_, err = svc.Save(context.Background(), id, SharedPoolAutoTransferUpdate{Threshold: 1, DailyTime: "12:00"})
		require.ErrorIs(t, err, ErrSharedPoolAutoTransferInvalid)
	}
	for _, in := range []SharedPoolAutoTransferUpdate{{Threshold: math.NaN(), DailyTime: "12:00"}, {Threshold: 0, DailyTime: "12:00"}, {Threshold: 1, DailyTime: "1:00"}} {
		_, err := svc.Save(context.Background(), 71, in)
		require.ErrorIs(t, err, ErrSharedPoolAutoTransferInvalid)
	}
}

func TestSharedPoolAutoTransferWorkerSkipsContendedLeader(t *testing.T) {
	leader := &autoTransferLeaderFake{}
	svc := NewSharedPoolAutoTransferService(&autoTransferRepoFake{}, nil, nil, leader, nil)
	t.Cleanup(svc.Stop)
	require.NoError(t, svc.runOnce(context.Background(), time.Now()))
	require.Equal(t, 1, leader.acquire)
	require.Zero(t, leader.release)
	require.Equal(t, sharedPoolAutoTransferLockKey, leader.key)
	require.Equal(t, svc.instanceID, leader.owner)
}

func TestSharedPoolAutoTransferWorkerRetriesFailureAndSkipsNilTransfers(t *testing.T) {
	now := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	scans := 0
	var calls []int64
	repo := &autoTransferRepoFake{
		due: func(_ context.Context, got time.Time, after int64, limit int) ([]int64, error) {
			require.Equal(t, now, got)
			require.Zero(t, after)
			require.Equal(t, 100, limit)
			scans++
			if scans == 1 {
				return []int64{10, 20, 30}, nil
			}
			return []int64{10}, nil
		},
		run: func(_ context.Context, id int64, got time.Time) (*SharedPoolEarningsTransfer, error) {
			require.Equal(t, now, got)
			calls = append(calls, id)
			if id == 10 && scans == 1 {
				return nil, errors.New("temporary transfer failure")
			}
			if id == 20 {
				return nil, nil
			}
			return &SharedPoolEarningsTransfer{ID: id, Amount: 2}, nil
		},
	}
	cache := &autoTransferCacheFake{balanceErr: errors.New("temporary cache failure")}
	leader := &autoTransferLeaderFake{acquired: true}
	svc := NewSharedPoolAutoTransferService(repo, cache, cache, leader, nil)
	t.Cleanup(svc.Stop)
	require.NoError(t, svc.runOnce(context.Background(), now))
	require.Equal(t, []int64{10, 20, 30}, calls)
	require.Equal(t, []int64{30}, cache.auth)
	require.Equal(t, []int64{30}, cache.balance)
	require.NoError(t, svc.runOnce(context.Background(), now))
	require.Equal(t, []int64{10, 20, 30, 10}, calls)
	require.Equal(t, []int64{30, 10}, cache.auth)
	require.Equal(t, []int64{30, 10}, cache.balance)
	require.Equal(t, 2, leader.release)
}

func TestSharedPoolAutoTransferWorkerInvalidatesAfterCommittedCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := &autoTransferRepoFake{run: func(context.Context, int64, time.Time) (*SharedPoolEarningsTransfer, error) {
		cancel()
		return &SharedPoolEarningsTransfer{ID: 1, Amount: 3}, nil
	}}
	cache := &autoTransferCacheFake{}
	svc := NewSharedPoolAutoTransferService(repo, cache, cache, nil, nil)
	t.Cleanup(svc.Stop)
	svc.runUser(ctx, 71, time.Now())
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Equal(t, []int64{71}, cache.auth)
	require.Equal(t, []int64{71}, cache.balance)
	require.Equal(t, []error{nil, nil}, cache.contextErrors)
	require.Equal(t, []bool{true, true}, cache.withDeadlines)
}

func TestSharedPoolAutoTransferWorkerLifecycle(t *testing.T) {
	t.Run("stop before start", func(t *testing.T) {
		svc := NewSharedPoolAutoTransferService(&autoTransferRepoFake{}, nil, nil, nil, nil)
		stopped := make(chan struct{})
		go func() { svc.Stop(); svc.Stop(); svc.Start(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(time.Second):
			t.Fatal("stop before start did not return")
		}
	})
	t.Run("start once and concurrent stop", func(t *testing.T) {
		started := make(chan struct{})
		var scans atomic.Int32
		repo := &autoTransferRepoFake{due: func(ctx context.Context, _ time.Time, _ int64, _ int) ([]int64, error) {
			if scans.Add(1) == 1 {
				close(started)
			}
			<-ctx.Done()
			return nil, ctx.Err()
		}}
		svc := NewSharedPoolAutoTransferService(repo, nil, nil, nil, nil)
		t.Cleanup(svc.Stop)
		svc.Start()
		svc.Start()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("worker did not start its first scan")
		}
		stopped := make(chan struct{}, 2)
		for range 2 {
			go func() { svc.Stop(); stopped <- struct{}{} }()
		}
		for range 2 {
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("worker did not stop on cancellation")
			}
		}
		require.Equal(t, int32(1), scans.Load())
	})
}
