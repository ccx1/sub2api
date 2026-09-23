package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// 复用账号串行队列及共享调度租约；软到期只复验原凭据，不重复增加库存。
func (s *OpenAIGatewayService) revalidateExistingCodexTicket(ctx context.Context, input *openAICodexTicketProbeInput) bool {
	if schedule := codexTicketScheduleFrom(ctx); schedule != nil && schedule.reservation.HalfOpen {
		return false
	}
	old, pending := s.codexTicketRevalidationTarget(input.Account, input.Model, *input.Config)
	if old == nil {
		return false
	}
	now := time.Now()
	if pending == nil && codexTicketPersistentCookieExpired(old, now) {
		return false
	}
	jar := newOpenAICodexTicketCookieJarFromSnapshot(old.Cookies, now, old.CookieSessionKeys)
	if pending != nil {
		jar = pending.Candidate.Jar
	}
	jar = codexTicketRevalidationCookieJar(jar, now)
	candidate := codexTicketRevalidationSnapshot(old, jar, *input.Config, now)
	if candidate == nil {
		if pending != nil {
			s.clearPendingCodexTicketCookies(old.AccountID, old.Model, pending)
		}
		return false
	}
	input.Attempt.Reason = "controls_changed"
	defer func() {
		// 候选可能在第二轮再次被拒绝；统一清理，但不删除并发到达的新候选。
		if pending != nil && codexTicketRevalidationRejected(input.Attempt.Reason) {
			s.clearPendingCodexTicketCookies(old.AccountID, old.Model, pending)
		}
	}()
	if err := s.admitCodexTicketProbe(ctx, *input); err != nil {
		return true
	}
	input.Revalidation, input.CookieJar = true, jar
	sessionID := candidate.SessionID
	input.SessionID = &sessionID
	input.BusinessCredentialSnapshot = candidate
	input.CookieCandidate = &openAICodexTicketCookieCandidate{}
	if !s.verifyOpenAICodexTicketBusiness(ctx, input, old.State) {
		// 瞬时传输/限流错误不撤旧票；候选失败不能误伤原 Cookie 快照。
		if pending == nil && codexTicketRevalidationRejected(input.Attempt.Reason) {
			s.invalidateOpenAICodexTicket(ctx, input.Account, old)
		}
		return true
	}
	if input.CookieCandidate.Jar != nil {
		candidate = codexTicketRevalidationSnapshot(old, input.CookieCandidate.Jar, *input.Config, time.Now())
		if candidate == nil {
			input.Attempt.Reason = "business_ticket_rejected"
			return true
		}
		candidate.SessionID = sessionID
		if input.CookieCandidate.HeaderChanged {
			input.CookieJar = input.CookieCandidate.Jar
			input.BusinessCredentialSnapshot = candidate
			input.CookieCandidate = &openAICodexTicketCookieCandidate{}
			input.SkipSchedulerAdmission = true
			if !s.verifyOpenAICodexTicketBusiness(ctx, input, old.State) {
				return true
			}
			if input.CookieCandidate.HeaderChanged {
				input.Attempt.Reason = "business_ticket_rejected"
				return true
			}
			if input.CookieCandidate.Jar != nil {
				candidate = codexTicketRevalidationSnapshot(old, input.CookieCandidate.Jar, *input.Config, time.Now())
				if candidate == nil {
					input.Attempt.Reason = "business_ticket_rejected"
					return true
				}
			}
		}
	}
	if !s.openAICodexTicketProbeConfigCurrent(ctx, *input) || !s.codexTicketAccountCurrentBeforePublish(ctx, *input) {
		return true
	}
	if schedule := codexTicketScheduleFrom(ctx); schedule != nil {
		if err := schedule.scheduler.ValidateCodexTicket(ctx, schedule.reservation); err != nil {
			return true
		}
	}
	candidate.Binding = s.codexTicketBindingForConfig(input.Account, *input.Config)
	candidate.SessionID = sessionID
	candidate.Egress = openAICodexTicketEgress(input.ProxyURL)
	candidate.AttemptID = input.Attempt.ID
	input.Attempt.Reason = "publish_failed"
	if s.replaceRevalidatedCodexTicket(ctx, input.Account, old, candidate, pending) {
		input.Attempt.Success, input.Attempt.Reason = true, "verified"
		input.Attempt.TicketCapturedAt, input.Attempt.TicketExpiresAt = &candidate.CapturedAt, &candidate.ExpiresAt
		if pending != nil {
			s.clearPendingCodexTicketCookies(old.AccountID, old.Model, pending)
		}
	}
	return true
}

func codexTicketRevalidationRejected(reason string) bool {
	return reason == "business_model_mismatch" || reason == "business_response_model_mismatch" || reason == "business_ticket_rejected"
}

func codexTicketPersistentCookieExpired(ticket *openAICodexTicket, now time.Time) bool {
	sessions := make(map[string]bool, len(ticket.CookieSessionKeys))
	for _, key := range ticket.CookieSessionKeys {
		sessions[key] = true
	}
	for _, cookie := range ticket.Cookies {
		if cookie != nil && !sessions[codexTicketCookieKey(*cookie)] && !cookie.Expires.IsZero() && !cookie.Expires.After(now) {
			return true
		}
	}
	return false
}

func codexTicketRevalidationCookieJar(jar http.CookieJar, now time.Time) http.CookieJar {
	copy, ok := cloneOpenAICodexTicketCookieJar(jar).(*codexTicketCookieJar)
	if !ok {
		return jar
	}
	// 只在未发布的探测副本上推进未知寿命，不改变服务器给定的期限。
	for key, entry := range copy.entries {
		if entry.cookie.Expires.IsZero() {
			entry.at = now
			copy.entries[key] = entry
		}
	}
	return copy
}

func (s *OpenAIGatewayService) codexTicketRevalidationTarget(account *Account, model string, cfg config.OpenAICodexTicketConfig) (*openAICodexTicket, *codexTicketPendingCookies) {
	key := openAICodexTicketKey(account.ID, model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	inventory := s.availableCodexTicketInventory(key, s.codexTicketInventoryLocked(account, model))
	if pending := s.pendingCodexTicketCookies(account.ID, model); pending != nil && codexTicketInventoryContains(inventory, pending.Ticket) && !s.codexTicketRevoked(key, pending.Ticket) {
		return codexTicketLeaf(pending.Ticket), pending
	}
	hydrateCodexTicketSoftRevalidate(inventory, cfg)
	for _, slot := range codexTicketSlots(inventory) {
		if slot.Revoked || !slot.accountCompatible(account) || !slot.Verified && !slot.VerificationSkipped {
			continue
		}
		if slot.needsRefresh(time.Now(), time.Duration(cfg.RefreshBeforeSeconds)*time.Second) {
			return codexTicketLeaf(slot), nil
		}
	}
	return nil, nil
}

// 这里只构造探测副本。未知期限的兼容延续，必须经过真实业务成功才能发布。
func codexTicketRevalidationSnapshot(old *openAICodexTicket, jar http.CookieJar, cfg config.OpenAICodexTicketConfig, now time.Time) *openAICodexTicket {
	copy := codexTicketLeaf(old)
	if copy == nil || copy.Revoked || !copy.StateExpiresAt.IsZero() && !copy.StateExpiresAt.After(now) {
		return nil
	}
	ttl := time.Duration(cfg.TTLSeconds) * time.Second
	copy.ExpiresAt, copy.RevalidateAt = now.Add(ttl), now.Add(ttl)
	if !copy.StateExpiresAt.IsZero() {
		copy.ExpiresAt = copy.StateExpiresAt
	}
	if copy.usesCookies() {
		if hard := snapshotCodexTicketCookieHardExpiresAt(jar); !hard.IsZero() && !hard.After(now) {
			return nil
		}
		cookieTTL := time.Duration(cfg.CookieTTLSeconds) * time.Second
		if cookieTTL <= 0 {
			cookieTTL = ttl
		}
		copy.Cookies, _, copy.ExpiresAt = snapshotCodexTicketCookieLifetime(jar, cookieTTL)
		copy.CookieSessionKeys = snapshotCodexTicketCookieSessionKeys(jar)
		copy.ExpiresAt = earlierCodexTicketExpiry(copy.ExpiresAt, copy.StateExpiresAt)
		if copy.CredentialMode == config.CodexTicketCredentialCookieState && copy.StateExpiresAt.IsZero() {
			copy.ExpiresAt = earlierCodexTicketExpiry(copy.ExpiresAt, now.Add(ttl))
		}
		copy.RevalidateAt = now.Add(min(ttl, cookieTTL))
		if len(copy.Cookies) == 0 {
			return nil
		}
	}
	copy.RevalidateAt = earlierCodexTicketExpiry(copy.RevalidateAt, copy.ExpiresAt)
	copy.CapturedAt, copy.RevalidatedAt = now, now
	copy.Verified, copy.VerificationSkipped = true, false
	if copy.usesCookies() && !copy.cookieUsable(now, cfg) || !copy.ExpiresAt.After(now) {
		return nil
	}
	if !copy.usesCookies() && !strings.HasPrefix(copy.State, openAICodexTicketStatePrefix) {
		return nil
	}
	return copy
}
