package service

import (
	"context"
	"errors"
	"math/rand/v2"
	"net/http"
	"slices"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

func recordCodexTicketProtectionResponse(in openAICodexTicketProbeInput, resp *http.Response) {
	if codexTicketProbeBusiness(in) || in.Attempt == nil || in.Config == nil || config.CodexTicketUsesCookies(*in.Config) || resp == nil || resp.StatusCode != http.StatusOK {
		return
	}
	policy := in.Config.TicketProtection()
	state := extractOpenAICodexTurnState(resp.Header)
	in.Attempt.protectionLengthHit = policy.Enabled && codexTicketAutoStateShape(state) && slices.Contains(policy.RejectAndSilenceLengths, len(state))
}

// 正文校验失败仍保留票头命中事实；明确账号额度/鉴权错误优先于静默规则。
func (s *OpenAIGatewayService) recordScheduledCodexHarvest(ctx context.Context, in openAICodexTicketProbeInput, state string, status int, cause error) bool {
	schedule := codexTicketScheduleFrom(ctx)
	if schedule == nil {
		return true
	}
	schedule.upstream = codexTicketProbeUpstreamError(cause)
	neutral := ctx.Err() != nil || errors.Is(cause, context.Canceled) || errors.Is(cause, context.DeadlineExceeded) || errors.Is(cause, errOpenAICodexTicketControlsChanged)
	var transport *codexTicketTransportError
	var rejected *openAICodexTicketProbeRejected
	if errors.As(cause, &transport) || errors.As(cause, &rejected) {
		neutral = true
	}
	if detail := schedule.upstream; detail != nil && (detail.EffectiveStatus == 401 || detail.EffectiveStatus == 403 || detail.EffectiveStatus == 429) {
		neutral = true
	}
	schedule.silence = in.Attempt.protectionLengthHit && !neutral
	schedule.accepted = cause == nil && ctx.Err() == nil && status == http.StatusOK && !schedule.silence && codexTicketProbeCandidateAccepted(in, state)
	schedule.harvestProxyFailed = codexTicketProxyAttemptFailed(ctx, status, cause, !schedule.accepted)
	if codexTicketQualityEnabled(in) {
		schedule.qualityProxyFailed = schedule.harvestProxyFailed
	}
	if !schedule.started {
		return true
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := schedule.scheduler.ReportCodexTicketHarvest(writeCtx, schedule.reservation, schedule.accepted, schedule.silence, neutral); err != nil {
		in.Attempt.Reason = "controls_changed"
		return false
	}
	if schedule.silence {
		in.Attempt.Reason = "harvest_protection_rejected"
		return false
	}
	return true
}

func codexTicketScheduledProtected(ctx context.Context) bool {
	schedule := codexTicketScheduleFrom(ctx)
	return schedule != nil && schedule.reservation.Config.TicketProtection().Enabled
}

func (s *OpenAIGatewayService) usesSharedCodexProtection(ctx context.Context) bool {
	_, available := s.accountRepo.(CodexTicketScheduler)
	return available && s.openAICodexTicketConfigContext(ctx).TicketProtection().Enabled
}

func (s *OpenAIGatewayService) finishCodexTicketSchedule(ctx context.Context, input *openAICodexTicketProbeInput) {
	schedule := codexTicketScheduleFrom(ctx)
	if schedule == nil {
		return
	}
	attempt := input.Attempt
	request := CodexTicketFinishRequest{Reservation: schedule.reservation, Started: schedule.started,
		HarvestAccepted: schedule.accepted, Silence: schedule.silence, Outcome: attempt.Outcome,
		HarvestProxyFailed: schedule.harvestProxyFailed, BusinessProxyFailed: schedule.businessProxyFailed,
		QualityProxyFailed:     schedule.qualityProxyFailed,
		BusinessProxySucceeded: schedule.businessProxySucceeded}
	request.TargetsComplete = attempt.Success && s.codexTicketTargetsComplete(input.Account, schedule.reservation.Config.Models)
	if schedule.deferred != nil {
		request.DeferredProxyID = schedule.deferred.ProxyID
		if schedule.deferred.RetryAt != nil {
			request.DeferredUntil = *schedule.deferred.RetryAt
		}
	}
	if codexTicketScheduledProtected(ctx) {
		codexTicketSharedRetry(&request, schedule.upstream)
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := schedule.scheduler.FinishCodexTicket(writeCtx, request); err != nil {
		logger.L().Warn("openai_codex_ticket scheduler finish failed", zap.Int64("account_id", input.Account.ID))
		return
	}
	// 使用本次原子收尾的结果，释放租约后的 GET 可能已属于下一次尝试。
	attempt.Protection = schedule.reservation.Status
}

func (s *OpenAIGatewayService) codexTicketTargetsComplete(account *Account, models []string) bool {
	cfg := s.openAICodexTicketConfig()
	for _, model := range models {
		if !s.lookupOpenAICodexTicketForConfig(account, model, cfg).usable(time.Now(), account, cfg) {
			return false
		}
	}
	return true
}

func codexTicketSharedRetry(request *CodexTicketFinishRequest, detail *CodexTicketUpstreamError) {
	if detail == nil {
		return
	}
	cfg := request.Reservation.Config
	seconds := 0
	switch detail.EffectiveStatus {
	case 401, 403:
		seconds = cfg.AuthCooldownSeconds
	case 429:
		seconds = cfg.RateLimitCooldownSeconds
	default:
		return
	}
	request.RetryScope = "model"
	if detail.Scope == "credential" {
		request.RetryScope = "account"
	}
	request.RetryAt = time.Now().Add(time.Duration(max(seconds, cfg.HarvestProbeIntervalSeconds)) * time.Second)
	if cfg.RespectRetryAfter && detail.RetryAt != nil && detail.RetryAt.After(request.RetryAt) {
		request.RetryAt = *detail.RetryAt
	}
	request.RetryAt = codexTicketRetryWithJitter(request.RetryAt, time.Now())
}

func codexTicketRetryWithJitter(until, now time.Time) time.Time {
	cap := min(2*time.Second, until.Sub(now)/4)
	if cap <= 0 {
		return until
	}
	return until.Add(time.Duration(rand.Int64N(int64(cap) + 1)))
}
