package service

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func codexTicketRouteHost(ticket *openAICodexTicket) string {
	if ticket == nil {
		return ""
	}
	for _, cookie := range ticket.Cookies {
		if cookie == nil || cookie.Name != codexOAILBCookieName {
			continue
		}
		claims, _, err := decodeCodexOAILBCookie(cookie.Value)
		if err != nil {
			continue
		}
		host, _ := claims["host"].(string)
		return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	}
	return ""
}

// A fresh jar makes the upstream assign a new route. Historical credentials
// deliberately bypass upstream revalidation after their protocol soft deadline.
func (s *OpenAIGatewayService) probeHistoricalTicketRoute(ctx context.Context, account *Account, ticket *openAICodexTicket, cfg config.OpenAICodexTicketConfig) (*openAICodexTicket, string, error) {
	if ticket == nil || strings.TrimSpace(ticket.SessionID) == "" {
		return nil, "", ErrOpenAICodexTicketUnavailable
	}
	if err := ResolveRandomProxyFromSource(ctx, account, s.accountRepo); err != nil {
		return nil, "", ErrOpenAICodexTicketUnavailable
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil || token == "" {
		return nil, "", ErrOpenAICodexTicketUnavailable
	}
	jar := newOpenAICodexTicketCookieJar()
	sessionID := ticket.SessionID
	input := openAICodexTicketProbeInput{Account: account, Token: token, Model: ticket.Model,
		ProxyURL: resolveAccountProxyURL(account), Timeout: time.Duration(cfg.HarvestAttemptTimeoutSeconds) * time.Second,
		Config: &cfg, SubscriptionTier: openAICodexTicketSubscriptionTier(account), SessionID: &sessionID,
		CookieJar: jar, CheckControls: true, SkipSchedulerAdmission: true}
	if _, status, err := s.probeOpenAICodexTicket(ctx, input); err != nil || status != http.StatusOK {
		return nil, "", ErrOpenAICodexTicketUnavailable
	}
	cookies, captured, expires := snapshotCodexTicketCookieLifetime(jar, probeCookieTTL(input))
	rotated := codexTicketLeaf(ticket)
	rotated.OriginCapturedAt = ticket.lineageCapturedAt()
	rotated.Cookies, rotated.CookieSessionKeys = cookies, snapshotCodexTicketCookieSessionKeys(jar)
	rotated.CapturedAt = captured
	if !expires.IsZero() {
		rotated.ExpiresAt = expires
	}
	host := codexTicketRouteHost(rotated)
	if host == "" || len(cookies) == 0 {
		return nil, "", ErrOpenAICodexTicketUnavailable
	}
	return rotated, host, nil
}

func (s *OpenAIGatewayService) historicalRouteSnapshot(account *Account, model string, cfg config.OpenAICodexTicketConfig) ([]*openAICodexTicket, string, string) {
	key := openAICodexTicketKey(account.ID, model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	inventory := s.availableCodexTicketInventory(key, s.codexTicketInventoryLocked(account, model))
	limitCodexTicketInventory(inventory, config.CodexTicketModelCapacity(cfg, model))
	id, host := codexTicketRouteRotationActive(inventory)
	return codexTicketUsageCandidates(inventory, account, cfg, time.Now()), id, host
}

// The inventory and its rotation metadata share one CAS write. A concurrent
// selection makes the caller resnapshot instead of publishing a stale route.
func (s *OpenAIGatewayService) commitHistoricalRoute(ctx context.Context, account *Account, model string, cfg config.OpenAICodexTicketConfig,
	candidate, rotated *openAICodexTicket, expectedID, expectedHost, host string) (bool, error) {
	key := openAICodexTicketKey(account.ID, model)
	lock := s.codexTicketLock(key)
	lock.Lock()
	defer lock.Unlock()
	current := s.availableCodexTicketInventory(key, s.codexTicketInventoryLocked(account, model))
	id, currentHost := codexTicketRouteRotationActive(current)
	if id != expectedID || currentHost != expectedHost || !codexTicketInventoryContains(current, candidate) ||
		!codexTicketRouteRotationCooldownUntil(current, candidate, time.Now()).IsZero() {
		return false, nil
	}
	if expectedHost != "" && host == expectedHost {
		codexTicketRouteRotationCool(current, candidate, time.Now().Add(time.Duration(cfg.SameRouteCooldownHours)*time.Hour))
	} else {
		if rotated != nil {
			for _, slot := range codexTicketSlots(current) {
				if sameCodexTicket(slot, candidate) {
					standby, reserve, rotation := slot.Standby, slot.Reserve, slot.RouteRotation
					*slot = *rotated
					slot.Standby, slot.Reserve, slot.RouteRotation = standby, reserve, rotation
					break
				}
			}
		}
		codexTicketRouteRotationSetActive(current, candidate, host)
	}
	updated, err := s.persistOpenAICodexTicketResult(ctx, account, model, current)
	if err != nil {
		return false, ErrOpenAICodexTicketUnavailable
	}
	if !updated {
		return false, nil
	}
	s.openaiCodexTickets.Store(key, current)
	account.Extra[openAICodexTicketExtraKey(model)] = current
	return true, nil
}

// A switch probes candidate tickets with fresh cookies until a different host
// is verified. Same-host tickets stay in inventory and regain eligibility after
// their independent cooldown expires.
func (s *OpenAIGatewayService) selectHistoricalRouteTicket(ctx context.Context, account *Account, model string, cfg config.OpenAICodexTicketConfig) (*openAICodexTicket, error) {
	if s == nil || account == nil {
		return nil, ErrOpenAICodexTicketUnavailable
	}
	account = cloneOpenAICodexTicketAccount(account)
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			if repo, ok := s.accountRepo.(codexTicketAccountReloader); ok {
				latest, err := repo.GetCodexTicketAccountSnapshot(ctx, account.ID)
				if err != nil || latest == nil {
					return nil, ErrOpenAICodexTicketUnavailable
				}
				account = latest
			}
		}
		candidates, activeID, activeHost := s.historicalRouteSnapshot(account, model, cfg)
		if activeID != "" {
			for _, candidate := range candidates {
				if codexTicketConsumptionID(candidate) == activeID {
					selected := codexTicketLeaf(candidate)
					selected.historicalExpiresAt = selected.historicalExpires(cfg)
					return selected, nil
				}
			}
		}
		stateChanged := false
		for _, candidate := range candidates {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			host, rotated := codexTicketRouteHost(candidate), (*openAICodexTicket)(nil)
			if activeID != "" || host == "" {
				var err error
				rotated, host, err = s.probeHistoricalTicketRoute(ctx, account, candidate, cfg)
				if err != nil {
					continue
				}
			}
			committed, err := s.commitHistoricalRoute(ctx, account, model, cfg, candidate, rotated, activeID, activeHost, host)
			if err != nil {
				return nil, err
			}
			if !committed {
				stateChanged = true
				break
			}
			if activeHost != "" && host == activeHost {
				continue
			}
			selected := candidate
			if rotated != nil {
				selected = rotated
			}
			selected = codexTicketLeaf(selected)
			selected.historicalExpiresAt = selected.historicalExpires(cfg)
			return selected, nil
		}
		if !stateChanged {
			return nil, ErrOpenAICodexTicketUnavailable
		}
	}
	return nil, ErrOpenAICodexTicketUnavailable
}
