package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

var ErrCodexTicketBusinessHoldLost = errors.New("codex ticket business lease lost")

const codexTicketBusinessHoldTTL = 90 * time.Second

type CodexTicketBusinessLease struct {
	AccountID int64
	Token     string
	TTL       time.Duration
}

// 真实 repository 必须使用共享 Redis；只有旧实现及单元测试可使用本地回退。
type CodexTicketBusinessHoldStore interface {
	AcquireCodexTicketBusinessHold(context.Context, CodexTicketBusinessLease) error
	RenewCodexTicketBusinessHold(context.Context, CodexTicketBusinessLease) error
	ReleaseCodexTicketBusinessHold(context.Context, CodexTicketBusinessLease) error
	CodexTicketBusinessHoldUntil(context.Context, int64) (time.Time, error)
}

type codexTicketHarvestProbeKey struct{}

func withCodexTicketHarvestProbe(ctx context.Context) context.Context {
	return context.WithValue(ctx, codexTicketHarvestProbeKey{}, true)
}

func (s *OpenAIGatewayService) codexTicketBusinessHoldStore() CodexTicketBusinessHoldStore {
	if store, ok := s.accountRepo.(CodexTicketBusinessHoldStore); ok {
		return store
	}
	return &s.openaiCodexTicketBusinessHolds
}

func (s *OpenAIGatewayService) beginCodexTicketBusinessHold(ctx context.Context, account *Account) (context.Context, func(), error) {
	probe, _ := ctx.Value(codexTicketHarvestProbeKey{}).(bool)
	if probe || s == nil || !OpenAICodexTicketAccountEnabled(account) || !s.openAICodexTicketConfigContext(ctx).Enabled {
		return ctx, func() {}, nil
	}
	lease := CodexTicketBusinessLease{AccountID: account.ID, Token: uuid.NewString(), TTL: codexTicketBusinessHoldTTL}
	return startCodexTicketBusinessLease(ctx, s.codexTicketBusinessHoldStore(), lease)
}

// 早检用于队列展示；实际采集准入由 scheduler reserve 的 Lua 再原子检查。
func (s *OpenAIGatewayService) checkCodexTicketBusinessIdle(ctx context.Context, account *Account) error {
	if s == nil || account == nil {
		return nil
	}
	readCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	until, err := s.codexTicketBusinessHoldStore().CodexTicketBusinessHoldUntil(readCtx, account.ID)
	if err != nil {
		return &CodexTicketWaitError{Status: &CodexTicketRuntimeStatus{State: "waiting", Reason: "shared_state_unavailable"}}
	}
	if !until.IsZero() {
		return &CodexTicketWaitError{Status: &CodexTicketRuntimeStatus{State: "waiting", Reason: "business_active", RetryAt: &until}}
	}
	return nil
}

type codexTicketBusinessLeaseRuntime struct {
	store CodexTicketBusinessHoldStore
	lease CodexTicketBusinessLease
}

func startCodexTicketBusinessLease(ctx context.Context, store CodexTicketBusinessHoldStore, lease CodexTicketBusinessLease) (context.Context, func(), error) {
	acquireCtx, cancelAcquire := context.WithTimeout(ctx, 2*time.Second)
	err := store.AcquireCodexTicketBusinessHold(acquireCtx, lease)
	cancelAcquire()
	if err != nil {
		return ctx, nil, err
	}
	leaseCtx, cancel := context.WithCancelCause(ctx)
	var once sync.Once
	release := func() {
		once.Do(func() {
			cancel(context.Canceled)
			releaseCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer stop()
			_ = store.ReleaseCodexTicketBusinessHold(releaseCtx, lease)
		})
	}
	runtime := codexTicketBusinessLeaseRuntime{store: store, lease: lease}
	go runtime.maintain(leaseCtx, cancel, release)
	return leaseCtx, release, nil
}

func (r codexTicketBusinessLeaseRuntime) maintain(ctx context.Context, cancel context.CancelCauseFunc, release func()) {
	ticker := time.NewTicker(r.lease.TTL / 3)
	defer ticker.Stop()
	defer release()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			renewCtx, stop := context.WithTimeout(ctx, 2*time.Second)
			err := r.store.RenewCodexTicketBusinessHold(renewCtx, r.lease)
			stop()
			if err != nil {
				// 无法维持共享占用时取消当前业务，不能边失租边继续占用上游。
				cancel(ErrCodexTicketBusinessHoldLost)
				return
			}
		}
	}
}

type codexTicketBusinessHoldBody struct {
	io.ReadCloser
	release func()
}

func (b *codexTicketBusinessHoldBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.release()
	}
	return n, err
}

func (b *codexTicketBusinessHoldBody) Close() error {
	defer b.release()
	return b.ReadCloser.Close()
}

func attachCodexTicketBusinessHold(response *http.Response, err error, release func()) {
	if err != nil || response == nil || response.Body == nil {
		release()
		return
	}
	response.Body = &codexTicketBusinessHoldBody{ReadCloser: response.Body, release: release}
}
