package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

type guardBatchBudgetRepo struct {
	guardMemoryRepo
	contexts []context.Context
}

func (r *guardBatchBudgetRepo) UpsertStateIfUnchanged(ctx context.Context, state AccountTokenGuardState) (time.Time, error) {
	r.contexts = append(r.contexts, ctx)
	if state.AccountID == 1 {
		return time.Time{}, errors.New("test state write failure")
	}
	return r.guardMemoryRepo.UpsertStateIfUnchanged(ctx, state)
}

func (r *guardBatchBudgetRepo) PruneEvents(ctx context.Context, _ time.Time) error {
	r.contexts = append(r.contexts, ctx)
	return errors.New("test prune failure")
}

func TestAccountTokenGuardPersistStatesUsesSingleBoundedBudget(t *testing.T) {
	repo := &guardBatchBudgetRepo{}
	svc := &AccountTokenGuardService{repo: repo}
	started := time.Now()
	failures := svc.persistStates([]AccountTokenGuardState{{AccountID: 1}, {AccountID: 2}, {AccountID: 3}})
	if failures != 2 || len(repo.contexts) != 4 || repo.upserts != 2 {
		t.Fatalf("failures=%d operations=%d saved=%d", failures, len(repo.contexts), repo.upserts)
	}
	first := repo.contexts[0]
	deadline, ok := first.Deadline()
	if !ok || deadline.Sub(started) > 11*time.Second || deadline.Sub(started) < 9*time.Second {
		t.Fatalf("batch persistence deadline=%v present=%t", deadline, ok)
	}
	for _, ctx := range repo.contexts {
		if ctx != first {
			t.Fatal("persistence operation reset the batch budget")
		}
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("completed batch retained live context: %v", ctx.Err())
		}
	}
}
