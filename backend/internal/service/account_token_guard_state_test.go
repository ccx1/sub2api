package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func guardTestAccount(id int64) Account {
	return Account{ID: id, Name: fmt.Sprintf("guard%d@example.com", id),
		Type: AccountTypeOAuth, Platform: PlatformOpenAI, Status: StatusActive,
		Credentials: map[string]any{"access_token": fmt.Sprintf("token-%d", id)}}
}

func newGuardTestService(repo *guardMemoryRepo, accounts *guardMemoryAccounts, endpoint string) *AccountTokenGuardService {
	svc := NewAccountTokenGuardService(nil, repo, accounts, nil, nil, nil)
	cfg := defaultAccountTokenGuardConfig()
	cfg.ProbeEndpoint, cfg.ReloginEndpoint = endpoint+"/probe", endpoint+"/relogin"
	cfg.ProbeConcurrency, cfg.MaxProbePerCycle, cfg.ProbeTimeoutSeconds = 1, len(accounts.items), 5
	cfg.AutoRelogin = false
	svc.config.Store(cfg)
	return svc
}

func guardStateForTest(t *testing.T, repo *guardMemoryRepo, id int64) AccountTokenGuardState {
	t.Helper()
	states, err := repo.ListStates(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range states {
		if state.AccountID == id {
			return state
		}
	}
	t.Fatalf("missing state for account %d", id)
	return AccountTokenGuardState{}
}

func TestAccountTokenGuardFailStreakAccumulatesThreeRounds(t *testing.T) {
	var relogins atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/relogin" {
			relogins.Add(1)
			_, _ = io.WriteString(w, `{"error":{"code":"relogin_rejected"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"failed","error":{"code":"invalid_token"}}`)
	}))
	defer server.Close()
	repo := &guardMemoryRepo{}
	account := guardTestAccount(1)
	svc := newGuardTestService(repo, &guardMemoryAccounts{items: []Account{account}}, server.URL)
	cfg := svc.currentConfig()
	cfg.AutoRelogin, cfg.FailStreakThreshold = true, 3
	cfg.ReloginAccounts = []AccountTokenGuardReloginAccount{{Email: account.Name, Password: "test-only"}}
	svc.config.Store(cfg)
	for round := 1; round <= 3; round++ {
		stats, err := svc.RunCycle(context.Background(), false)
		if err != nil {
			t.Fatal(err)
		}
		state := guardStateForTest(t, repo, account.ID)
		if state.FailStreak != round || stats.AuthFailed != 1 {
			t.Fatalf("round=%d state=%+v stats=%+v", round, state, stats)
		}
		wantLogins := int64(0)
		if round == 3 {
			wantLogins = 1
		}
		if relogins.Load() != wantLogins {
			t.Fatalf("round=%d relogins=%d want=%d", round, relogins.Load(), wantLogins)
		}
	}
}

func TestAccountTokenGuardTransientPreservesAndHealthyClearsStreak(t *testing.T) {
	var healthy atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if healthy.Load() {
			_, _ = io.WriteString(w, `{"status":"active"}`)
			return
		}
		_, _ = io.WriteString(w, `{"error":{"code":"provider_unauthorized"}}`)
	}))
	defer server.Close()
	fixedAt := time.Now().Add(-time.Hour)
	repo := &guardMemoryRepo{states: []AccountTokenGuardState{{AccountID: 1, FailStreak: 2,
		LastFixAt: &fixedAt, LastFixAction: "previous repair", LastFixResult: "retained"}}}
	svc := newGuardTestService(repo, &guardMemoryAccounts{items: []Account{guardTestAccount(1)}}, server.URL)
	for _, want := range []int{2, 0} {
		if _, err := svc.RunCycle(context.Background(), false); err != nil {
			t.Fatal(err)
		}
		state := guardStateForTest(t, repo, 1)
		if state.FailStreak != want || state.LastFixAt == nil || !state.LastFixAt.Equal(fixedAt) || state.LastFixResult != "retained" {
			t.Fatalf("want streak %d with retained repair history, got %+v", want, state)
		}
		healthy.Store(true)
	}
}

func TestAccountTokenGuardPartialCyclesPreserveHistoryAndReachEveryAccount(t *testing.T) {
	var mu sync.Mutex
	probed := map[string]bool{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		probed[payload["access_token"]] = true
		mu.Unlock()
		_, _ = io.WriteString(w, `{"status":"active"}`)
	}))
	defer server.Close()
	accounts := &guardMemoryAccounts{}
	for id := int64(1); id <= 5; id++ {
		accounts.items = append(accounts.items, guardTestAccount(id))
	}
	repo := &guardMemoryRepo{}
	svc := newGuardTestService(repo, accounts, server.URL)
	cfg := svc.currentConfig()
	cfg.MaxProbePerCycle = 2
	svc.config.Store(cfg)
	for round := 0; round < 3; round++ {
		if _, err := svc.RunCycle(context.Background(), false); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(probed) != 5 || len(repo.states) != 5 || repo.deleteCalls != 0 {
		t.Fatalf("probed=%v retained=%d pruning=%d", probed, len(repo.states), repo.deleteCalls)
	}
}

func TestAccountTokenGuardHistoryReadFailureStopsBeforeProbe(t *testing.T) {
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprintf("read%d", failAt), func(t *testing.T) {
			var probes atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				probes.Add(1)
				_, _ = io.WriteString(w, `{"status":"active"}`)
			}))
			defer server.Close()
			wantErr := errors.New("history unavailable")
			repo := &guardMemoryRepo{listErr: wantErr, listErrAfter: failAt}
			svc := newGuardTestService(repo, &guardMemoryAccounts{items: []Account{guardTestAccount(1)}}, server.URL)
			_, err := svc.RunCycle(context.Background(), false)
			if !errors.Is(err, wantErr) || probes.Load() != 0 || repo.upserts != 0 {
				t.Fatalf("err=%v probes=%d upserts=%d", err, probes.Load(), repo.upserts)
			}
		})
	}
}

func TestAccountTokenGuardManualReloginRequiresHistoryRead(t *testing.T) {
	wantErr := errors.New("history unavailable")
	repo := &guardMemoryRepo{listErr: wantErr}
	svc := newGuardTestService(repo, &guardMemoryAccounts{items: []Account{guardTestAccount(1)}}, "http://127.0.0.1:1")
	if _, err := svc.ReloginAccount(context.Background(), 1); !errors.Is(err, wantErr) {
		t.Fatalf("expected history error before relogin, got %v", err)
	}
}
