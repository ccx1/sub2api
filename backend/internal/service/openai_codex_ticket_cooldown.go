package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const openAICodexTicketCooldownSweepKey = "codex_ticket_cooldown_sweep"

type openAICodexTicketProbeRejected struct {
	Status     int
	RetryAfter string
}

func (*openAICodexTicketProbeRejected) Error() string {
	return "codex ticket probe rejected by upstream"
}

func (s *OpenAIGatewayService) openAICodexTicketCooling(account *Account, token string) bool {
	key := openAICodexTicketCooldownKey(account, token)
	if s == nil || key == "" {
		return false
	}
	now := time.Now()
	s.sweepOpenAICodexTicketCooldown(now)
	raw, ok := s.openaiCodexTicketCooldown.Load(key)
	if !ok {
		return false
	}
	until, ok := raw.(time.Time)
	if !ok || !now.Before(until) {
		s.openaiCodexTicketCooldown.CompareAndDelete(key, raw)
		return false
	}
	return true
}

func (s *OpenAIGatewayService) coolOpenAICodexTicket(account *Account, token string, err error) {
	var rejected *openAICodexTicketProbeRejected
	if s == nil || !errors.As(err, &rejected) || rejected == nil ||
		(rejected.Status != http.StatusUnauthorized && rejected.Status != http.StatusForbidden && rejected.Status != http.StatusTooManyRequests) {
		return
	}
	key := openAICodexTicketCooldownKey(account, token)
	if key == "" {
		return
	}
	now := time.Now()
	s.sweepOpenAICodexTicketCooldown(now)
	cfg := s.openAICodexTicketConfig()
	seconds := cfg.AuthCooldownSeconds
	if rejected.Status == http.StatusTooManyRequests {
		seconds = cfg.RateLimitCooldownSeconds
	}
	retryAfter := ""
	if cfg.RespectRetryAfter {
		retryAfter = rejected.RetryAfter
	}
	until := codexTicketConfiguredCooldownDeadline(now, max(seconds, cfg.HarvestProbeIntervalSeconds), retryAfter)
	lock := s.codexTicketLock("cooldown:" + key)
	lock.Lock()
	defer lock.Unlock()
	if raw, ok := s.openaiCodexTicketCooldown.Load(key); ok {
		if previous, ok := raw.(time.Time); ok && previous.After(until) {
			return
		}
	}
	s.openaiCodexTicketCooldown.Store(key, until)
}

func openAICodexTicketCooldownKey(account *Account, token string) string {
	if account == nil || account.ID <= 0 || strings.TrimSpace(token) == "" {
		return ""
	}
	// 模型不参与身份；旧凭据的迟到回调只能延长旧身份，不覆盖新凭据状态。
	identity, _ := json.Marshal([]string{token, account.GetCredential("refresh_token"), account.GetChatGPTAccountID()})
	digest := sha256.Sum256(identity)
	return strconv.FormatInt(account.ID, 10) + ":" + hex.EncodeToString(digest[:])
}

func openAICodexTicketCooldownDeadline(now time.Time, intervalSeconds int, retryAfter string) time.Time {
	return codexTicketConfiguredCooldownDeadline(now, max(intervalSeconds, 300), retryAfter)
}

func codexTicketConfiguredCooldownDeadline(now time.Time, seconds int, retryAfter string) time.Time {
	const maxSeconds = int64((1<<63 - 1) / time.Second)
	until := now.Add(time.Duration(min(int64(seconds), maxSeconds)) * time.Second)
	retryAfter = strings.TrimSpace(retryAfter)
	if delta, err := strconv.ParseUint(retryAfter, 10, 64); err == nil {
		candidate := now.Add(time.Duration(min(delta, uint64(maxSeconds))) * time.Second)
		if candidate.After(until) {
			return candidate
		}
	} else if candidate, err := http.ParseTime(retryAfter); err == nil && candidate.After(until) {
		return candidate
	}
	return until
}

func (s *OpenAIGatewayService) sweepOpenAICodexTicketCooldown(now time.Time) {
	next := now.Add(time.Minute)
	raw, loaded := s.openaiCodexTicketCooldown.LoadOrStore(openAICodexTicketCooldownSweepKey, next)
	if loaded {
		previous, ok := raw.(time.Time)
		if !ok || now.Before(previous) || !s.openaiCodexTicketCooldown.CompareAndSwap(openAICodexTicketCooldownSweepKey, raw, next) {
			return
		}
	}
	s.openaiCodexTicketCooldown.Range(func(key, raw any) bool {
		if key != openAICodexTicketCooldownSweepKey {
			if until, ok := raw.(time.Time); ok && !now.Before(until) {
				// 清理过程中可能刚被延长，不能删除新的冷却期限。
				s.openaiCodexTicketCooldown.CompareAndDelete(key, raw)
			}
		}
		return true
	})
}
