//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	httppool "github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/stretchr/testify/require"
)

type sharedUsageProbeTransport func(*http.Request) (*http.Response, error)

func (f sharedUsageProbeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func sharedUsageSuccessfulProbeClient(t *testing.T, proxy *Proxy) {
	t.Helper()
	client, err := httppool.GetClient(httppool.Options{
		ProxyURL: proxy.URL(), Timeout: 15 * time.Second, ResponseHeaderTimeout: 10 * time.Second,
	})
	require.NoError(t, err)
	original := client.Transport
	// 使用独占本地代理地址的客户端替身，不访问公网或改变其它连接池条目。
	client.Transport = sharedUsageProbeTransport(func(req *http.Request) (*http.Response, error) {
		headers := make(http.Header)
		headers.Set("x-codex-primary-used-percent", "25")
		headers.Set("x-codex-primary-reset-after-seconds", "18000")
		headers.Set("x-codex-primary-window-minutes", "300")
		return &http.Response{StatusCode: http.StatusOK, Header: headers, Request: req,
			Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	t.Cleanup(func() { client.Transport = original })
}

func TestSharedUsageProbeWaitsForSnapshotBeforeReturning(t *testing.T) {
	proxy := sharedUsageLocalProxy(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(502) })
	sharedUsageSuccessfulProbeClient(t, proxy)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	repo := &sharedUsageProbeRepo{proxy: proxy}
	repo.write = func(ctx context.Context, id int64, updates map[string]any) error {
		close(entered)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	svc := &AccountUsageService{accountRepo: repo}
	account := sharedUsageProbeAccount(RandomProxyEmptyPoolPolicyReject)
	finished := make(chan error, 1)
	go func() {
		updates, err := svc.probeOpenAICodexSnapshot(context.Background(), account)
		if err == nil && updates["codex_5h_used_percent"] != float64(25) {
			err = errors.New("missing fetched usage window")
		}
		finished <- err
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not begin snapshot persistence")
	}
	select {
	case err := <-finished:
		t.Fatalf("probe returned before persistence completed: %v", err)
	case <-time.After(30 * time.Millisecond):
	}
	unblock()
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("probe did not finish after persistence completed")
	}
	require.Nil(t, account.ProxyID)
	require.Nil(t, account.Proxy)
}

func TestSharedUsageSnapshotSurvivesCancellationAndReportsWriteFailure(t *testing.T) {
	for _, failWrite := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		want := errors.New("snapshot write failed")
		calls := 0
		repo := &sharedUsageProbeRepo{write: func(writeCtx context.Context, id int64, updates map[string]any) error {
			calls++
			require.NoError(t, writeCtx.Err())
			deadline, ok := writeCtx.Deadline()
			require.True(t, ok)
			require.WithinDuration(t, time.Now().Add(5*time.Second), deadline, time.Second)
			require.Equal(t, int64(111), id)
			require.Equal(t, map[string]any{"codex_5h_used_percent": 25.0}, updates)
			if failWrite {
				return want
			}
			return nil
		}}
		svc := &AccountUsageService{accountRepo: repo}
		err := svc.persistOpenAIUsageProbeSnapshot(ctx, sharedUsageProbeAccount(""), map[string]any{"codex_5h_used_percent": 25.0})
		if failWrite {
			require.ErrorIs(t, err, want)
		} else {
			require.NoError(t, err)
		}
		require.Equal(t, 1, calls)
	}
}

func TestSharedUsageSnapshotKeepsOrdinaryAccountPersistenceAsynchronous(t *testing.T) {
	entered, release, persisted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	repo := &sharedUsageProbeRepo{write: func(ctx context.Context, _ int64, _ map[string]any) error {
		close(entered)
		defer close(persisted)
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}}
	svc := &AccountUsageService{accountRepo: repo}
	account := sharedUsageProbeAccount("")
	delete(account.Extra, SharedPoolOwnerKey)
	finished := make(chan error, 1)
	go func() {
		finished <- svc.persistOpenAIUsageProbeSnapshot(context.Background(), account, map[string]any{"codex_5h_used_percent": 25.0})
	}()
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("ordinary account waited for asynchronous persistence")
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("ordinary account did not persist its snapshot")
	}
	unblock()
	select {
	case <-persisted:
	case <-time.After(2 * time.Second):
		t.Fatal("ordinary account persistence did not complete")
	}
}
