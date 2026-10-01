package service

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"time"
)

func guardSameAccountVersion(left, right *Account) bool {
	if left == nil || right == nil {
		return false
	}
	l, _ := json.Marshal(left.Credentials)
	r, _ := json.Marshal(right.Credentials)
	return guardAccountEligible(left) && left.ID == right.ID && left.UpdatedAt.Equal(right.UpdatedAt) &&
		left.Status == right.Status && left.Schedulable == right.Schedulable &&
		reflect.DeepEqual(left.ProxyID, right.ProxyID) && string(l) == string(r)
}

func (a *guardMemoryAccounts) ListTokenGuardCandidates(_ context.Context, groups []int64) ([]Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	var result []Account
	for _, account := range a.items {
		if !guardAccountEligible(&account) {
			continue
		}
		matched := len(groups) == 0
		for _, group := range groups {
			matched = matched || slices.Contains(account.GroupIDs, group)
		}
		if matched {
			result = append(result, *guardAccountSnapshot(&account))
		}
	}
	return result, nil
}

func (a *guardMemoryAccounts) ApplyTokenGuardRepair(ctx context.Context, expected *Account, credentials map[string]any) (time.Time, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	for i := range a.items {
		current := &a.items[i]
		if !guardSameAccountVersion(current, expected) {
			continue
		}
		if credentials != nil {
			current.Credentials = credentials
		}
		current.Status, current.ErrorMessage = StatusActive, ""
		current.UpdatedAt = current.UpdatedAt.Add(time.Microsecond)
		return current.UpdatedAt, nil
	}
	return time.Time{}, ErrAccountTokenGuardStale
}

func (r *guardMemoryRepo) UpsertStateIfUnchanged(_ context.Context, state AccountTokenGuardState) (time.Time, error) {
	if r.accounts != nil {
		r.accounts.mu.Lock()
		defer r.accounts.mu.Unlock()
		matched := false
		for i := range r.accounts.items {
			matched = matched || guardSameAccountVersion(&r.accounts.items[i], state.AccountVersion)
		}
		if !matched {
			return time.Time{}, ErrAccountTokenGuardStale
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	found := false
	for _, previous := range r.states {
		if previous.AccountID != state.AccountID {
			continue
		}
		found = true
		if !previous.UpdatedAt.Equal(state.UpdatedAt) {
			return time.Time{}, ErrAccountTokenGuardStale
		}
	}
	if !found && !state.UpdatedAt.IsZero() {
		return time.Time{}, ErrAccountTokenGuardStale
	}
	version := time.Now().Truncate(time.Microsecond)
	if !version.After(state.UpdatedAt) {
		version = state.UpdatedAt.Add(time.Microsecond)
	}
	state.UpdatedAt = version
	err := r.upsertLocked(state)
	return state.UpdatedAt, err
}
