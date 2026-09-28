package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const guardSuccessCredentials = `{"credential":{"access_token":"new","refresh_token":"refresh","id_token":"id"}}`

func enableGuardReloginForTest(svc *AccountTokenGuardService, accounts ...Account) {
	cfg := svc.currentConfig()
	cfg.AutoRelogin = true
	cfg.FailStreakThreshold = 1
	for _, account := range accounts {
		cfg.ReloginAccounts = append(cfg.ReloginAccounts, AccountTokenGuardReloginAccount{Email: account.Name, Password: "test-only"})
	}
	svc.config.Store(cfg)
}

func TestAccountTokenGuardSnapshotDeepCopiesCredentials(t *testing.T) {
	account := guardTestAccount(1)
	account.Credentials["nested"] = map[string]any{"token": "old"}
	copy := guardAccountSnapshot(&account)
	account.Credentials["nested"].(map[string]any)["token"] = "new"
	account.Credentials["access_token"] = "changed"
	if copy.Credentials["nested"].(map[string]any)["token"] != "old" || copy.Credentials["access_token"] != "token-1" {
		t.Fatal("snapshot shares credential maps")
	}
}

type guardLegacyAccounts struct{ accountTokenGuardAccounts }
type guardLegacyStateRepo struct{ AccountTokenGuardRepository }

func TestAccountTokenGuardMissingDedicatedStoreFailsClosed(t *testing.T) {
	for _, missing := range []string{"accounts", "states"} {
		t.Run(missing, func(t *testing.T) {
			svc := newGuardTestService(&guardMemoryRepo{}, &guardMemoryAccounts{items: []Account{guardTestAccount(1)}}, "http://127.0.0.1:1")
			if missing == "accounts" {
				svc.accounts = &guardLegacyAccounts{svc.accounts}
			} else {
				svc.repo = &guardLegacyStateRepo{svc.repo}
			}
			if _, err := svc.RunCycle(context.Background(), false); !errors.Is(err, ErrAccountTokenGuardStoreUnavailable) {
				t.Fatalf("missing %s err=%v", missing, err)
			}
		})
	}
}

func TestAccountTokenGuardReloginDiscardsChangedAccount(t *testing.T) {
	mutations := map[string]func(*guardMemoryAccounts){
		"credentials": func(a *guardMemoryAccounts) { a.items[0].Credentials["access_token"] = "admin" },
		"status":      func(a *guardMemoryAccounts) { a.items[0].Status = "inactive" },
		"schedulable": func(a *guardMemoryAccounts) { a.items[0].Schedulable = false },
		"proxy":       func(a *guardMemoryAccounts) { id := int64(8); a.items[0].ProxyID = &id },
		"version":     func(a *guardMemoryAccounts) { a.items[0].UpdatedAt = a.items[0].UpdatedAt.Add(time.Second) },
		"deleted":     func(a *guardMemoryAccounts) { a.items = nil },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			account := guardTestAccount(1)
			accounts := &guardMemoryAccounts{items: []Account{account}}
			repo := &guardMemoryRepo{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				accounts.mu.Lock()
				mutate(accounts)
				accounts.mu.Unlock()
				_, _ = io.WriteString(w, guardSuccessCredentials)
			}))
			defer server.Close()
			svc := newGuardTestService(repo, accounts, server.URL)
			enableGuardReloginForTest(svc, account)
			if _, err := svc.ReloginAccount(context.Background(), 1); !errors.Is(err, ErrAccountTokenGuardStale) {
				t.Fatalf("err=%v", err)
			}
			if repo.upserts != 0 {
				t.Fatalf("stale relogin persisted state %d times", repo.upserts)
			}
			if len(accounts.items) > 0 && accounts.items[0].Credentials["access_token"] == "new" {
				t.Fatal("stale credentials overwrote administrator")
			}
		})
	}
}

func TestAccountTokenGuardStateConflictDoesNotOverwriteHistory(t *testing.T) {
	account := guardTestAccount(1)
	repo := &guardMemoryRepo{}
	accounts := &guardMemoryAccounts{items: []Account{account}}
	svc := newGuardTestService(repo, accounts, "http://127.0.0.1:1")
	state := AccountTokenGuardState{AccountID: 1}
	guardStateUsesAccount(&state, &account)
	if err := svc.storeGuardState(context.Background(), &state); err != nil {
		t.Fatal(err)
	}
	stale := state
	state.FailStreak = 9
	if err := svc.storeGuardState(context.Background(), &state); err != nil {
		t.Fatal(err)
	}
	stale.FailStreak = 1
	if err := svc.storeGuardState(context.Background(), &stale); !errors.Is(err, ErrAccountTokenGuardStale) {
		t.Fatalf("err=%v", err)
	}
	if got := guardStateForTest(t, repo, 1); got.FailStreak != 9 {
		t.Fatalf("state=%+v", got)
	}
}

type guardBlockingInvalidator struct{ entered, release chan struct{} }

func (i *guardBlockingInvalidator) InvalidateToken(ctx context.Context, _ *Account) error {
	close(i.entered)
	select {
	case <-i.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestAccountTokenGuardCancelAfterCommittedRepairPersistsSuccess(t *testing.T) {
	for _, mode := range []string{"manual", "relogin", "recovery"} {
		t.Run(mode, func(t *testing.T) {
			account := guardTestAccount(1)
			account.Schedulable = false
			account.Status = StatusError
			accounts := &guardMemoryAccounts{items: []Account{account}}
			repo := &guardMemoryRepo{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/relogin" {
					_, _ = io.WriteString(w, guardSuccessCredentials)
				} else if mode == "recovery" {
					_, _ = io.WriteString(w, `{"status":"active"}`)
				} else {
					_, _ = io.WriteString(w, `{"error":{"code":"invalid_token"}}`)
				}
			}))
			defer server.Close()
			svc := newGuardTestService(repo, accounts, server.URL)
			enableGuardReloginForTest(svc, account)
			invalidator := &guardBlockingInvalidator{make(chan struct{}), make(chan struct{})}
			svc.invalidator = invalidator
			if mode == "manual" {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				done := make(chan error, 1)
				go func() { _, err := svc.ReloginAccount(ctx, 1); done <- err }()
				awaitGuardSignal(t, invalidator.entered)
				cancel()
				close(invalidator.release)
				if err := <-done; !errors.Is(err, context.Canceled) {
					t.Fatalf("err=%v", err)
				}
			} else {
				job, err := svc.StartRun(true)
				if err != nil {
					t.Fatal(err)
				}
				awaitGuardSignal(t, invalidator.entered)
				if _, err := svc.CancelRun(job.ID); err != nil {
					t.Fatal(err)
				}
				close(invalidator.release)
				finished := awaitGuardJob(t, svc, job.ID)
				if finished.Status != AccountTokenGuardJobCanceled || finished.Stats.Failed != 0 || finished.Stats.Repaired+finished.Stats.StateFixed != 1 {
					t.Fatalf("job=%+v", finished)
				}
			}
			state := guardStateForTest(t, repo, 1)
			if state.LastFixAt == nil || state.AccountStatus != StatusActive || state.Schedulable || state.LastFixResult == "" {
				t.Fatalf("committed repair lost state=%+v", state)
			}
			if !state.AccountVersion.UpdatedAt.Equal(accounts.items[0].UpdatedAt) {
				t.Fatal("state did not use committed account version")
			}
			svc.Stop()
		})
	}
}
