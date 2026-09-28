package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func awaitGuardSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal("guard operation did not start")
	}
}

func awaitGuardJob(t *testing.T, svc *AccountTokenGuardService, id string) *AccountTokenGuardJob {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		job, ok := svc.Job(id)
		if ok && job.Status != AccountTokenGuardJobPending && job.Status != AccountTokenGuardJobRunning {
			return job
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("guard job did not finish")
	return nil
}

func TestAccountTokenGuardCancelStopsQueuedProbes(t *testing.T) {
	for _, stopService := range []bool{false, true} {
		name := "cancel"
		if stopService {
			name = "stop"
		}
		t.Run(name, func(t *testing.T) {
			entered := make(chan struct{}, 1)
			var calls atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				calls.Add(1)
				select {
				case entered <- struct{}{}:
				default:
				}
				<-r.Context().Done()
			}))
			defer server.Close()
			accounts := &guardMemoryAccounts{}
			for id := int64(1); id <= 10; id++ {
				accounts.items = append(accounts.items, guardTestAccount(id))
			}
			repo := &guardMemoryRepo{}
			svc := newGuardTestService(repo, accounts, server.URL)
			defer svc.Stop()
			job, err := svc.StartRun(true)
			if err != nil {
				t.Fatal(err)
			}
			awaitGuardSignal(t, entered)
			if stopService {
				svc.Stop()
			} else if _, err := svc.CancelRun(job.ID); err != nil {
				t.Fatal(err)
			}
			finished := awaitGuardJob(t, svc, job.ID)
			if finished.Status != AccountTokenGuardJobCanceled || finished.Stats.Probed != 1 || calls.Load() != 1 {
				t.Fatalf("job=%+v HTTP calls=%d", finished, calls.Load())
			}
			if repo.upserts != 0 || svc.runtimeInfo().Stats.StartedAt == 0 {
				t.Fatalf("canceled probes wrote state or left stale runtime: writes=%d", repo.upserts)
			}
		})
	}
}

func TestAccountTokenGuardCancelDuringReloginStaysCanceled(t *testing.T) {
	entered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/probe" {
			_, _ = io.WriteString(w, `{"error":{"code":"invalid_token"}}`)
			return
		}
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case entered <- struct{}{}:
		default:
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	account := guardTestAccount(1)
	svc := newGuardTestService(&guardMemoryRepo{}, &guardMemoryAccounts{items: []Account{account}}, server.URL)
	defer svc.Stop()
	cfg := svc.currentConfig()
	cfg.AutoRelogin, cfg.FailStreakThreshold = true, 1
	cfg.ReloginAccounts = []AccountTokenGuardReloginAccount{{Email: account.Name, Password: "test-only"}}
	svc.config.Store(cfg)
	job, err := svc.StartRun(true)
	if err != nil {
		t.Fatal(err)
	}
	awaitGuardSignal(t, entered)
	if _, err := svc.CancelRun(job.ID); err != nil {
		t.Fatal(err)
	}
	finished := awaitGuardJob(t, svc, job.ID)
	if finished.Status != AccountTokenGuardJobCanceled || !finished.CancelRequested || finished.Stats.Failed != 0 || finished.Stats.Repaired != 0 {
		t.Fatalf("canceled relogin incorrectly finished as %+v", finished)
	}
}

type guardBlockingRecoveryAccounts struct {
	*guardMemoryAccounts
	entered chan struct{}
}

func (a *guardBlockingRecoveryAccounts) ApplyTokenGuardRepair(ctx context.Context, _ *Account, _ map[string]any) (time.Time, error) {
	select {
	case a.entered <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return time.Time{}, ctx.Err()
}

func TestAccountTokenGuardCancelDuringRecoveryStaysCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"active"}`)
	}))
	defer server.Close()
	account := guardTestAccount(1)
	account.Status = StatusError
	svc := newGuardTestService(&guardMemoryRepo{}, &guardMemoryAccounts{items: []Account{account}}, server.URL)
	defer svc.Stop()
	accounts := &guardBlockingRecoveryAccounts{guardMemoryAccounts: svc.accounts.(*guardMemoryAccounts), entered: make(chan struct{}, 1)}
	svc.accounts = accounts
	job, err := svc.StartRun(true)
	if err != nil {
		t.Fatal(err)
	}
	awaitGuardSignal(t, accounts.entered)
	if _, err := svc.CancelRun(job.ID); err != nil {
		t.Fatal(err)
	}
	finished := awaitGuardJob(t, svc, job.ID)
	if finished.Status != AccountTokenGuardJobCanceled || finished.Stats.Failed != 0 || finished.Stats.StateFixed != 0 {
		t.Fatalf("canceled recovery incorrectly finished as %+v", finished)
	}
}

func TestAccountTokenGuardStoppedServiceRejectsNewJobs(t *testing.T) {
	svc := newGuardTestService(&guardMemoryRepo{}, &guardMemoryAccounts{items: []Account{guardTestAccount(1)}}, "http://127.0.0.1:1")
	svc.Stop()
	if job, err := svc.StartRun(true); job != nil || err == nil {
		t.Fatalf("stopped service accepted job=%+v err=%v", job, err)
	}
}
