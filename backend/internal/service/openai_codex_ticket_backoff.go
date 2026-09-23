package service

import (
	"context"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

const codexTicketBackoffRetention = 24 * time.Hour
const codexTicketBackoffSweepKey = "codex_ticket_backoff_sweep"

type codexTicketBackoffState struct {
	Failures  int
	RetryAt   time.Time
	UpdatedAt time.Time
	Exhausted bool
}

func codexTicketBackoffDelay(failures int, cfg config.OpenAICodexTicketConfig) time.Duration {
	delays := cfg.RetryBackoffSeconds
	if len(delays) == 0 {
		return time.Duration(cfg.HarvestProbeIntervalSeconds) * time.Second
	}
	return time.Duration(delays[max(0, min(failures-1, len(delays)-1))]) * time.Second
}

func codexTicketBackoffKey(account *Account, token, model string) string {
	identity := openAICodexTicketCooldownKey(account, token)
	model = normalizeOpenAICodexTicketModel(model)
	if identity == "" || model == "" {
		return ""
	}
	// 不绑定代理，避免失败换出口后立刻绕过退避；各模型独立恢复。
	return identity + ":" + model
}

func (s *OpenAIGatewayService) openAICodexTicketBackoffActive(account *Account, token, model string) bool {
	if s == nil {
		return false
	}
	now := time.Now()
	s.sweepCodexTicketBackoff(now)
	raw, ok := s.openaiCodexTicketBackoff.Load(codexTicketBackoffKey(account, token, model))
	state, valid := raw.(*codexTicketBackoffState)
	return ok && valid && now.Before(state.RetryAt)
}

func (s *OpenAIGatewayService) finishOpenAICodexTicketHarvest(ctx context.Context, input *openAICodexTicketProbeInput) {
	attempt := input.Attempt
	if attempt == nil {
		return
	}
	attempt.Outcome = codexTicketAttemptOutcome(attempt)
	if schedule := codexTicketScheduleFrom(ctx); schedule != nil && schedule.upstream != nil && !attempt.Success && !attempt.canceled && attempt.Reason != "controls_changed" && schedule.deferred == nil {
		attempt.Outcome = "upstream_error"
	}
	s.finishCodexTicketSchedule(ctx, input)
	if attempt.StartedAt.IsZero() {
		return
	}
	sharedRejection := codexTicketScheduleFrom(ctx) != nil && attempt.Outcome == "ticket_rejected"
	if attempt.Success || sharedRejection {
		s.clearCodexTicketAccountBackoff(input.Account, input.Token)
	}
	if !sharedRejection && !codexTicketScheduledProtected(ctx) && (attempt.Success || !attempt.canceled && attempt.Reason != "controls_changed" && attempt.Reason != "verification_deferred" && s.openAICodexTicketProbeAllowed(ctx, input.Account, input.Token) && s.openAICodexTicketProbeConfigCurrent(ctx, *input)) {
		s.advanceCodexTicketBackoff(input, time.Now())
	}
	s.finishCodexTicketAttempt(ctx, input.Account.ID, attempt)
}

func (s *OpenAIGatewayService) clearCodexTicketAccountBackoff(account *Account, token string) {
	identity := openAICodexTicketCooldownKey(account, token)
	if identity == "" {
		return
	}
	s.openaiCodexTicketBackoff.Range(func(key, value any) bool {
		if name, ok := key.(string); ok && strings.HasPrefix(name, identity+":") {
			s.openaiCodexTicketBackoff.CompareAndDelete(key, value)
		}
		return true
	})
}

func (s *OpenAIGatewayService) advanceCodexTicketBackoff(input *openAICodexTicketProbeInput, now time.Time) {
	key := codexTicketBackoffKey(input.Account, input.Token, input.Model)
	if key == "" {
		return
	}
	s.sweepCodexTicketBackoff(now)
	lock := s.codexTicketLock("backoff:" + key)
	lock.Lock()
	defer lock.Unlock()
	if input.Attempt.Success {
		s.openaiCodexTicketBackoff.Delete(key)
		return
	}
	cfg := s.openAICodexTicketConfig()
	failures := 1
	if raw, ok := s.openaiCodexTicketBackoff.Load(key); ok {
		if previous, ok := raw.(*codexTicketBackoffState); ok && now.Before(previous.UpdatedAt.Add(codexTicketBackoffRetention)) && !previous.Exhausted {
			failures = previous.Failures + 1
		}
	}
	exhausted := cfg.RetryMaxAttempts > 0 && failures >= cfg.RetryMaxAttempts
	delay := codexTicketBackoffDelay(failures, cfg)
	if exhausted {
		delay = time.Duration(cfg.RetryExhaustedCooldownSeconds) * time.Second
	}
	state := &codexTicketBackoffState{Failures: failures, RetryAt: now.Add(delay), UpdatedAt: now, Exhausted: exhausted}
	s.openaiCodexTicketBackoff.Store(key, state)
	logger.L().Info("openai_codex_ticket retry cooldown", zap.Int64("account_id", input.Account.ID),
		zap.String("model", input.Model), zap.Int("consecutive_failures", failures),
		zap.Bool("retry_exhausted", exhausted),
		zap.Int("cooldown_seconds", int(delay/time.Second)), zap.Time("retry_at", state.RetryAt))
}

func (s *OpenAIGatewayService) sweepCodexTicketBackoff(now time.Time) {
	raw, loaded := s.openaiCodexTicketBackoff.LoadOrStore(codexTicketBackoffSweepKey, now.Add(time.Minute))
	if loaded {
		previous, ok := raw.(time.Time)
		if !ok || now.Before(previous) || !s.openaiCodexTicketBackoff.CompareAndSwap(codexTicketBackoffSweepKey, raw, now.Add(time.Minute)) {
			return
		}
	}
	s.openaiCodexTicketBackoff.Range(func(key, raw any) bool {
		if state, ok := raw.(*codexTicketBackoffState); ok && !now.Before(state.UpdatedAt.Add(codexTicketBackoffRetention)) {
			s.openaiCodexTicketBackoff.CompareAndDelete(key, raw)
		}
		return true
	})
}
