//go:build unit

package handler

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSharedImportConcurrentUserGuardAndWorkerLimit(t *testing.T) {
	entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: "one\ntwo\nthree\nfour\nfive"}}, Defaults: importTestDefaults()})
	require.NoError(t, err)
	var active, maxActive atomic.Int32
	entered := make(chan struct{}, 5)
	release := make(chan struct{})
	done := make(chan *sharedImportResult, 1)
	create := func(ctx context.Context, ownerID int64, in service.SharedPoolAccountInput) (*service.SharedPoolAccountView, error) {
		n := active.Add(1)
		for previous := maxActive.Load(); n > previous && !maxActive.CompareAndSwap(previous, n); previous = maxActive.Load() {
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		active.Add(-1)
		return &service.SharedPoolAccountView{ID: ownerID, Name: in.Name}, nil
	}
	go func() { result, _ := executeSharedImport(context.Background(), 801, entries, create); done <- result }()
	for range 4 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("workers did not start")
		}
	}
	_, err = executeSharedImport(context.Background(), 801, entries, create)
	require.Equal(t, 429, infraerrors.Code(err))
	close(release)
	result := <-done
	require.Equal(t, 5, result.Created)
	require.EqualValues(t, 4, maxActive.Load())
	for i, item := range result.Items {
		require.Equal(t, i+1, item.Index)
	}
}

func TestSharedImportDuplicateOnlyFailsAndErrorsAreRedacted(t *testing.T) {
	entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: "duplicate\nduplicate\ninternal-error"}}, Defaults: importTestDefaults()})
	require.NoError(t, err)
	var mu sync.Mutex
	seen := map[string]bool{}
	create := func(_ context.Context, _ int64, in service.SharedPoolAccountInput) (*service.SharedPoolAccountView, error) {
		mu.Lock()
		defer mu.Unlock()
		token := in.Credentials["access_token"].(string)
		if token == "internal-error" {
			return nil, errors.New("SQL contains secret-password")
		}
		if seen[token] {
			return nil, infraerrors.Conflict("SHARED_ACCOUNT_EXISTS", "该凭证已加入共享池").WithCause(errors.New("secret-owner-id"))
		}
		seen[token] = true
		return &service.SharedPoolAccountView{ID: 7, Name: in.Name}, nil
	}
	result, err := executeSharedImport(context.Background(), 802, entries, create)
	require.NoError(t, err)
	require.Equal(t, 1, result.Created)
	require.Equal(t, 2, result.Failed)
	for _, item := range result.Items {
		require.NotContains(t, item.Message, "secret")
	}
	result, err = executeSharedImport(context.Background(), 802, entries, create)
	require.NoError(t, err)
	require.Zero(t, result.Created)
	require.Equal(t, 3, result.Failed)
}

func TestSharedImportCancelledBeforeCreation(t *testing.T) {
	entries, err := parseSharedImport(sharedImportRequest{Sources: []sharedImportSource{{Content: "token"}}, Defaults: importTestDefaults()})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := executeSharedImport(ctx, 803, entries, func(context.Context, int64, service.SharedPoolAccountInput) (*service.SharedPoolAccountView, error) {
		t.Fatal("unexpected write")
		return nil, nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, result.Failed)
}
