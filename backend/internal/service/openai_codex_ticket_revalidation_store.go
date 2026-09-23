package service

import (
	"context"
	"time"
)

func (s *OpenAIGatewayService) replaceRevalidatedCodexTicket(ctx context.Context, account *Account, old, next *openAICodexTicket, pending *codexTicketPendingCookies) bool {
	if s.settingService != nil {
		s.settingService.codexTicketPublishMu.RLock()
		defer s.settingService.codexTicketPublishMu.RUnlock()
	}
	key := openAICodexTicketKey(account.ID, old.Model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	inventory := s.availableCodexTicketInventory(key, s.codexTicketInventoryLocked(account, old.Model))
	if !codexTicketInventoryContains(inventory, old) || s.codexTicketRevoked(key, old) {
		return false
	}
	queued, _ := s.openaiCodexTicketPending.Load(key)
	if pending != nil && queued != pending.stored {
		return false
	}
	if latest, ok := queued.(*codexTicketPendingCookies); pending == nil && ok && sameCodexTicket(latest.Ticket, old) {
		// 探测期间来了更晚的 Cookie，保留队列，下一轮验证最新候选。
		return false
	}
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	next.AccountBinding = openAICodexTicketAccountBinding(account)
	if !cfg.Enabled || !next.usable(time.Now(), account, cfg) || next.Binding != s.codexTicketBindingForConfig(account, cfg) {
		return false
	}
	// 旧回调按 CapturedAt 撤票；每次发布递增版本，防止迟到响应撤销新 Cookie。
	if latest := codexTicketInventoryTime(inventory); !next.CapturedAt.After(latest) {
		next.CapturedAt = latest.Add(time.Nanosecond)
	}
	replacement := cloneCodexTicketInventory(inventory)
	for _, slot := range codexTicketSlots(replacement) {
		if sameCodexTicket(slot, old) {
			standby, reserve := slot.Standby, slot.Reserve
			*slot = *codexTicketLeaf(next)
			slot.Standby, slot.Reserve = standby, reserve
			break
		}
	}
	// CAS 必须比较读取时的原始 JSON，不能用 hydrate 后新增字段的票作 expected。
	if !s.persistOpenAICodexTicket(ctx, cloneOpenAICodexTicketAccount(account), old.Model, replacement) {
		return false
	}
	s.openaiCodexTickets.Store(key, replacement)
	return true
}
