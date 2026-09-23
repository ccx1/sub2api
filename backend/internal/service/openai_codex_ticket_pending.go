package service

import (
	"net/http"
	"sync"
	"time"
)

const codexTicketPendingLimit = 1024

// 容量检查与新键写入串行；读取不取此锁，避免与库存分片锁形成反向依赖。
var codexTicketPendingCapacityMu sync.Mutex

type codexTicketPendingCookies struct {
	Ticket    *openAICodexTicket
	Candidate *openAICodexTicketCookieCandidate
	ExpiresAt time.Time
	stored    *codexTicketPendingCookies
}

func (s *OpenAIGatewayService) captureCodexTicketCookieCandidate(receipt *openAICodexTicketReceipt, req *http.Request, resp *http.Response) {
	if s == nil || receipt == nil || receipt.account == nil || req == nil || resp == nil ||
		!receipt.ticket.usesCookies() || !receipt.ticket.matchesHeaders(req.Header) ||
		receipt.account.ID <= 0 || receipt.account.ID != receipt.ticket.AccountID || len(resp.Cookies()) == 0 {
		return
	}
	codexTicketPendingCapacityMu.Lock()
	defer codexTicketPendingCapacityMu.Unlock()
	now := time.Now()
	count := s.prunePendingCodexTicketCookies(now)
	key := openAICodexTicketKey(receipt.account.ID, receipt.ticket.Model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	inventory := s.availableCodexTicketInventory(key, s.codexTicketInventoryLocked(receipt.account, receipt.ticket.Model))
	if !pendingCodexTicketReceiptCurrent(inventory, &receipt.ticket) {
		return
	}
	previous, _ := s.openaiCodexTicketPending.Load(key)
	if previous == nil && count >= codexTicketPendingLimit {
		return
	}
	entry, _ := previous.(*codexTicketPendingCookies)
	if entry != nil && !sameCodexTicket(entry.Ticket, &receipt.ticket) && !receipt.ticket.CapturedAt.After(entry.Ticket.CapturedAt) {
		return
	}
	base := newOpenAICodexTicketCookieJarFromSnapshot(receipt.ticket.Cookies, receipt.ticket.CapturedAt, receipt.ticket.CookieSessionKeys)
	if entry != nil && sameCodexTicket(entry.Ticket, &receipt.ticket) {
		base = entry.Candidate.Jar
	}
	ttl := probeCookieTTL(openAICodexTicketProbeInput{Config: &receipt.config})
	candidate, changed := candidateOpenAICodexTicketCookies(base, req, resp, ttl)
	if !changed {
		return
	}
	next := &codexTicketPendingCookies{Ticket: clonePendingCodexTicket(&receipt.ticket), Candidate: candidate,
		ExpiresAt: pendingCodexTicketCookieDeadline(&receipt.ticket, candidate, now)}
	if next.ExpiresAt.After(now) {
		s.openaiCodexTicketPending.Store(key, next)
	}
}

func pendingCodexTicketReceiptCurrent(inventory, ticket *openAICodexTicket) bool {
	for _, current := range codexTicketSlots(inventory) {
		if !current.Revoked && sameCodexTicket(current, ticket) {
			return true
		}
	}
	return false
}

func pendingCodexTicketCookieDeadline(ticket *openAICodexTicket, candidate *openAICodexTicketCookieCandidate, now time.Time) time.Time {
	deadline := now.Add(5 * time.Minute)
	for _, expires := range []time.Time{ticket.StateExpiresAt, candidate.ExpiresAt, candidate.HardExpiresAt} {
		if !expires.IsZero() && expires.Before(deadline) {
			deadline = expires
		}
	}
	return deadline
}

func (s *OpenAIGatewayService) pendingCodexTicketCookies(accountID int64, model string) *codexTicketPendingCookies {
	if s == nil {
		return nil
	}
	key := openAICodexTicketKey(accountID, model)
	raw, ok := s.openaiCodexTicketPending.Load(key)
	entry, valid := raw.(*codexTicketPendingCookies)
	if !ok || !valid {
		return nil
	}
	if !entry.ExpiresAt.After(time.Now()) {
		s.openaiCodexTicketPending.CompareAndDelete(key, entry)
		return nil
	}
	copy := *entry
	copy.Ticket = clonePendingCodexTicket(entry.Ticket)
	copy.Candidate = clonePendingCodexTicketCandidate(entry.Candidate)
	copy.stored = entry
	return &copy
}

func clonePendingCodexTicket(ticket *openAICodexTicket) *openAICodexTicket {
	copy := codexTicketLeaf(ticket)
	if copy != nil {
		copy.CookieSessionKeys = append([]string(nil), ticket.CookieSessionKeys...)
	}
	return copy
}

func clonePendingCodexTicketCandidate(candidate *openAICodexTicketCookieCandidate) *openAICodexTicketCookieCandidate {
	copy := *candidate
	copy.Jar = cloneOpenAICodexTicketCookieJar(candidate.Jar)
	copy.Cookies = codexTicketLeaf(&openAICodexTicket{Cookies: candidate.Cookies}).Cookies
	copy.SessionKeys = append([]string(nil), candidate.SessionKeys...)
	return &copy
}

func (s *OpenAIGatewayService) clearPendingCodexTicketCookies(accountID int64, model string, snapshot *codexTicketPendingCookies) bool {
	if s == nil || snapshot == nil || snapshot.stored == nil {
		return false
	}
	return s.openaiCodexTicketPending.CompareAndDelete(openAICodexTicketKey(accountID, model), snapshot.stored)
}

func (s *OpenAIGatewayService) prunePendingCodexTicketCookies(now time.Time) int {
	count := 0
	s.openaiCodexTicketPending.Range(func(key, value any) bool {
		entry, ok := value.(*codexTicketPendingCookies)
		if !ok || !entry.ExpiresAt.After(now) {
			s.openaiCodexTicketPending.CompareAndDelete(key, value)
		} else {
			count++
		}
		return true
	})
	return count
}
