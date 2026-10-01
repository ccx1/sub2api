package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

type guardDedicatedAccounts struct {
	*guardMemoryAccounts
	candidates []Account
}

func (a *guardDedicatedAccounts) ListTokenGuardCandidates(context.Context, []int64) ([]Account, error) {
	return a.candidates, nil
}

func TestAccountTokenGuardUsesDedicatedErrorAccountQuery(t *testing.T) {
	active, failed := guardTestAccount(1), guardTestAccount(2)
	failed.Status = StatusError
	accounts := &guardDedicatedAccounts{guardMemoryAccounts: &guardMemoryAccounts{items: []Account{active}}, candidates: []Account{active, failed}}
	svc := newGuardTestService(&guardMemoryRepo{}, accounts.guardMemoryAccounts, "http://127.0.0.1:1")
	svc.accounts = accounts
	cfg := svc.currentConfig()
	cfg.MaxProbePerCycle = 10
	got, err := svc.listAccounts(context.Background(), cfg)
	if err != nil || len(got) != 2 {
		t.Fatalf("dedicated query must include error accounts: got=%v err=%v", got, err)
	}
}

func TestAccountTokenGuardDisabledAccountNeverRelogins(t *testing.T) {
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = io.WriteString(w, `{"error":{"code":"relogin_rejected"}}`)
	}))
	defer server.Close()
	account := guardTestAccount(1)
	account.Status = "inactive"
	svc := newGuardTestService(&guardMemoryRepo{}, &guardMemoryAccounts{items: []Account{account}}, server.URL)
	cfg := svc.currentConfig()
	cfg.ReloginAccounts = []AccountTokenGuardReloginAccount{{Email: account.Name, Password: "test-only"}}
	svc.config.Store(cfg)
	if _, err := svc.ReloginAccount(context.Background(), account.ID); err == nil || calls.Load() != 0 {
		t.Fatalf("disabled account reached authorization: calls=%d err=%v", calls.Load(), err)
	}
}

func TestAccountTokenGuardDeduplicatesManualRelogin(t *testing.T) {
	entered, release := make(chan struct{}, 2), make(chan struct{})
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		_, _ = io.WriteString(w, `{"error":{"code":"relogin_rejected"}}`)
	}))
	defer server.Close()
	defer close(release)
	account := guardTestAccount(1)
	svc := newGuardTestService(&guardMemoryRepo{}, &guardMemoryAccounts{items: []Account{account}}, server.URL)
	cfg := svc.currentConfig()
	cfg.ReloginAccounts = []AccountTokenGuardReloginAccount{{Email: account.Name, Password: "test-only"}}
	svc.config.Store(cfg)
	done := make(chan struct{})
	go func() { _, _ = svc.ReloginAccount(context.Background(), account.ID); close(done) }()
	awaitGuardSignal(t, entered)
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := svc.ReloginAccount(ctx, account.ID); err == nil || calls.Load() != 1 {
		t.Errorf("duplicate relogin reached upstream: calls=%d err=%v", calls.Load(), err)
	}
	release <- struct{}{}
	awaitGuardSignal(t, done)
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if _, err := svc.ReloginAccount(ctx2, account.ID); errors.Is(err, ErrAccountTokenGuardBusy) {
		t.Fatal("failed login retained account lock")
	}
}

func TestAccountTokenGuardManualAndAutomaticShareAccountLock(t *testing.T) {
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/probe" {
			_, _ = io.WriteString(w, `{"error":{"code":"invalid_token"}}`)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		calls.Add(1)
		entered <- struct{}{}
		<-release
		_, _ = io.WriteString(w, guardSuccessCredentials)
	}))
	defer server.Close()
	defer close(release)
	account := guardTestAccount(1)
	svc := newGuardTestService(&guardMemoryRepo{}, &guardMemoryAccounts{items: []Account{account}}, server.URL)
	enableGuardReloginForTest(svc, account)
	job, err := svc.StartRun(true)
	if err != nil {
		t.Fatal(err)
	}
	awaitGuardSignal(t, entered)
	if _, err := svc.ReloginAccount(context.Background(), 1); !errors.Is(err, ErrAccountTokenGuardBusy) {
		t.Fatalf("err=%v", err)
	}
	release <- struct{}{}
	finished := awaitGuardJob(t, svc, job.ID)
	if calls.Load() != 1 || finished.Stats.Repaired != 1 {
		t.Fatalf("calls=%d job=%+v", calls.Load(), finished)
	}
}

func TestAccountTokenGuardDifferentAccountsReloginConcurrently(t *testing.T) {
	entered, release := make(chan struct{}, 2), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		entered <- struct{}{}
		<-release
		_, _ = io.WriteString(w, guardSuccessCredentials)
	}))
	defer server.Close()
	defer close(release)
	first, second := guardTestAccount(1), guardTestAccount(2)
	svc := newGuardTestService(&guardMemoryRepo{}, &guardMemoryAccounts{items: []Account{first, second}}, server.URL)
	enableGuardReloginForTest(svc, first, second)
	done := make(chan error, 2)
	for _, id := range []int64{1, 2} {
		go func(id int64) { _, err := svc.ReloginAccount(context.Background(), id); done <- err }(id)
	}
	awaitGuardSignal(t, entered)
	awaitGuardSignal(t, entered)
	release <- struct{}{}
	release <- struct{}{}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}
