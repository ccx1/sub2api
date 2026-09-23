package service

import (
	"context"
	"sync"
	"time"
)

type codexTicketLocalBusinessHolds struct {
	mu     sync.Mutex
	leases map[int64]map[string]time.Time
}

func (s *codexTicketLocalBusinessHolds) AcquireCodexTicketBusinessHold(ctx context.Context, lease CodexTicketBusinessLease) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leases == nil {
		s.leases = make(map[int64]map[string]time.Time)
	}
	if s.leases[lease.AccountID] == nil {
		s.leases[lease.AccountID] = make(map[string]time.Time)
	}
	s.leases[lease.AccountID][lease.Token] = time.Now().Add(lease.TTL)
	return nil
}

func (s *codexTicketLocalBusinessHolds) RenewCodexTicketBusinessHold(ctx context.Context, lease CodexTicketBusinessLease) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.leases[lease.AccountID][lease.Token].After(time.Now()) {
		return ErrCodexTicketBusinessHoldLost
	}
	s.leases[lease.AccountID][lease.Token] = time.Now().Add(lease.TTL)
	return nil
}

func (s *codexTicketLocalBusinessHolds) ReleaseCodexTicketBusinessHold(ctx context.Context, lease CodexTicketBusinessLease) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.leases[lease.AccountID], lease.Token)
	if len(s.leases[lease.AccountID]) == 0 {
		delete(s.leases, lease.AccountID)
	}
	return nil
}

func (s *codexTicketLocalBusinessHolds) CodexTicketBusinessHoldUntil(ctx context.Context, accountID int64) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var earliest time.Time
	for token, until := range s.leases[accountID] {
		if !until.After(time.Now()) {
			delete(s.leases[accountID], token)
			continue
		}
		if earliest.IsZero() || until.Before(earliest) {
			earliest = until
		}
	}
	if len(s.leases[accountID]) == 0 {
		delete(s.leases, accountID)
	}
	return earliest, nil
}
