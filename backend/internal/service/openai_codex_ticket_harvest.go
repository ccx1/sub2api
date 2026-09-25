package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

type codexTicketAccountReloader interface {
	GetCodexTicketAccountSnapshot(context.Context, int64) (*Account, error)
}

// 候选票必须完成模型检查；业务出口复验由管理端策略控制。
func (s *OpenAIGatewayService) probeOnceOpenAICodexTicket(ctx context.Context, account *Account, model string) {
	if s == nil || s.httpUpstream == nil || !isOpenAICodexTicketAccount(account, model) || !s.openAICodexTicketProbeAllowed(ctx, account, "") {
		return
	}
	if s.codexModelQualityCircuitPaused(ctx, account, model) {
		return
	}
	key := openAICodexTicketKey(account.ID, model)
	execute := func() (any, error) {
		s.enqueueCodexTicketProbe(ctx, account, model, isCodexTicketManualRetry(ctx))
		return nil, nil
	}
	if !isCodexTicketManualRetry(ctx) {
		_, _, _ = s.openaiCodexTicketFlight.Do(key, execute)
		return
	}
	// 每次手动点击独立排队，不能与自动采集合并而丢失请求。
	_, _ = execute()
}

func (s *OpenAIGatewayService) harvestVerifiedOpenAICodexTicket(ctx context.Context, account *Account, model string) {
	ctx = withCodexTicketHarvestProbe(ctx)
	account = s.reloadOpenAICodexTicketProbeAccount(ctx, account)
	if account == nil || !isOpenAICodexTicketAccount(account, model) {
		return
	}
	cfg := s.openAICodexTicketConfigForAccount(ctx, account)
	if !s.openAICodexTicketGatedModelContext(ctx, model) {
		return
	}
	if s.codexModelQualityCircuitPaused(ctx, account, model) {
		return
	}
	now := time.Now()
	// 自动任务只补不足或临期的库存，主票仍然服务当前业务。
	if !isCodexTicketManualRetry(ctx) && !s.codexTicketInventoryNeedsRefresh(account, model, cfg, now) {
		return
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil || strings.TrimSpace(token) == "" {
		return
	}
	if !s.usesSharedCodexProtection(ctx) && !isCodexTicketManualRetry(ctx) &&
		(s.openAICodexTicketCooling(account, token) || s.openAICodexTicketBackoffActive(account, token, model)) {
		return
	}
	ctx, proxy, err := s.reserveCodexTicketHarvest(ctx, account, model, cfg)
	if err != nil || (proxy.url == "" && proxy.policy.mode != CodexTicketProxyModeAccount) {
		return
	}
	binding, captured := s.codexTicketBindingForConfig(account, cfg), time.Now()
	attempt := newCodexTicketAttempt(model)
	var harvestSessionID string
	captureCodexTicketAttemptPolicy(attempt, account, cfg)
	if config.CodexTicketBusinessVerificationEnabled(cfg) {
		attempt.BusinessVerificationRounds = max(1, cfg.BusinessVerificationRounds)
	}
	attempt.HarvestProxy = codexTicketProxySnapshot(proxy.url, proxy.proxyID, proxy.proxyName)
	input := openAICodexTicketProbeInput{Account: account, Token: token, Model: model, ProxyURL: proxy.url,
		Timeout: time.Duration(cfg.HarvestAttemptTimeoutSeconds) * time.Second, CheckControls: true, Attempt: attempt,
		Config: &cfg, SubscriptionTier: openAICodexTicketSubscriptionTier(account), HarvestProxyPolicy: &proxy.policy,
		CookieJar: newOpenAICodexTicketCookieJar(), SessionID: &harvestSessionID, HarvestProxy: &proxy}
	if schedule := codexTicketScheduleFrom(ctx); schedule != nil {
		input.SessionEpoch = &schedule.reservation.SessionEpoch
	}
	defer s.finishOpenAICodexTicketHarvest(ctx, &input)
	if !isCodexTicketManualRetry(ctx) && cfg.RefreshStrategy != config.CodexTicketRefreshReplace && s.revalidateExistingCodexTicket(ctx, &input) {
		return
	}
	state, status, err := s.probeOpenAICodexTicket(ctx, input)
	if !s.recordScheduledCodexHarvest(ctx, input, state, status, err) {
		return
	}
	if err != nil {
		attempt.canceled = errors.Is(err, context.Canceled)
		if errors.Is(err, errOpenAICodexTicketControlsChanged) {
			attempt.Reason = "controls_changed"
			return
		}
		if !codexTicketScheduledProtected(ctx) {
			s.coolOpenAICodexTicket(account, token, err)
		}
		s.reportOpenAICodexTicketProxyFailure(ctx, proxy, err)
		var transport *codexTicketTransportError
		var rejected *openAICodexTicketProbeRejected
		if status > 0 && !errors.As(err, &transport) && !errors.As(err, &rejected) && s.openAICodexTicketProbeAllowed(ctx, account, token) {
			s.reportOpenAICodexTicketProxyResult(ctx, proxy, false)
		}
		if errors.As(err, &rejected) && (rejected.Status >= 500 || rejected.Status == http.StatusProxyAuthRequired) {
			s.reportOpenAICodexTicketProxyResult(ctx, proxy, false)
		}
		attempt.Reason = codexTicketAttemptErrorReason("harvest", err)
		return
	}
	if status != http.StatusOK || !codexTicketProbeCandidateAccepted(input, state) {
		attempt.Reason = codexTicketCandidateFailureReason(state, account, cfg)
		s.reportOpenAICodexTicketProxyResult(ctx, proxy, false)
		return
	}
	attempt.Reason = "controls_changed"
	var cookieCandidate openAICodexTicketCookieCandidate
	if config.CodexTicketUsesCookies(cfg) {
		input.CookieCandidate = &cookieCandidate
	}
	if !s.verifyOpenAICodexTicketBusiness(ctx, &input, state) {
		return
	}
	// A Set-Cookie received while verifying the old snapshot is only a
	// candidate. Re-run the business probe with that candidate before it can be
	// published, and keep any newer response cookies for a later cycle.
	if !input.FreezeCredentials && input.CookieCandidate != nil && cookieCandidate.Jar != nil {
		if cookieCandidate.HeaderChanged {
			input.CookieJar = cookieCandidate.Jar
			input.BusinessCredentialSnapshot = nil
			input.SkipSchedulerAdmission = true
			input.CookieCandidate = &openAICodexTicketCookieCandidate{}
			if !s.verifyOpenAICodexTicketBusiness(ctx, &input, state) {
				return
			}
			if input.CookieCandidate.HeaderChanged {
				attempt.Reason = "business_ticket_rejected"
				return
			}
			if input.CookieCandidate.Jar != nil {
				input.CookieJar = input.CookieCandidate.Jar
			}
		} else {
			input.CookieJar = cookieCandidate.Jar
		}
	}
	if !s.openAICodexTicketProbeAllowed(ctx, input.Account, input.Token) || !s.openAICodexTicketProbeConfigCurrent(ctx, input) ||
		binding != s.codexTicketBindingForConfig(input.Account, s.openAICodexTicketConfigContext(ctx)) || !s.codexTicketAccountCurrentBeforePublish(ctx, input) {
		return
	}
	if schedule := codexTicketScheduleFrom(ctx); schedule != nil {
		if err := schedule.scheduler.ValidateCodexTicket(ctx, schedule.reservation); err != nil {
			return
		}
	}
	verified := config.CodexTicketBusinessVerificationEnabled(cfg)
	ttl := time.Duration(cfg.TTLSeconds) * time.Second
	ticket := &openAICodexTicket{AttemptID: attempt.ID, AccountID: account.ID, Model: model, State: state, Length: len(state), CapturedAt: captured,
		ExpiresAt: captured.Add(ttl), RevalidateAt: captured.Add(ttl), Attempts: 1, Verified: verified, VerificationSkipped: !verified, Binding: binding,
		Egress: openAICodexTicketEgress(input.ProxyURL), HarvestProxyID: proxy.proxyID, HarvestProxyName: proxy.proxyName,
		SessionID: harvestSessionID, HarvestEgress: openAICodexTicketEgress(proxy.url)}
	if metadata, metadataErr := parseOpenAICodexTicketStateMetadata(state); metadataErr == nil && cfg.CredentialMode != config.CodexTicketCredentialCookie {
		ticket.IssuedAt = metadata.IssuedAt
		ticket.StateExpiresAt = codexTicketStateExpiresAt(metadata)
		ticket.ExpiresAt = ticket.StateExpiresAt
	}
	if config.CodexTicketUsesCookies(cfg) {
		ticket.CredentialMode = cfg.CredentialMode
		ticket.CookieSessionKeys = snapshotCodexTicketCookieSessionKeys(input.CookieJar)
		cookieTTL := probeCookieTTL(input)
		var cookieExpires time.Time
		ticket.Cookies, ticket.CapturedAt, cookieExpires = snapshotCodexTicketCookieLifetime(input.CookieJar, cookieTTL)
		if ticket.CapturedAt.IsZero() {
			ticket.CapturedAt = captured
		}
		if !cookieExpires.IsZero() {
			// A server supplied Cookie expiry is authoritative. The configured
			// Cookie TTL is only the fallback for session cookies without one.
			ticket.ExpiresAt = cookieExpires
		}
		revalidateTTL := ttl
		if cookieTTL > 0 && cookieTTL < revalidateTTL {
			revalidateTTL = cookieTTL
		}
		ticket.RevalidateAt = ticket.CapturedAt.Add(revalidateTTL)
		if !ticket.StateExpiresAt.IsZero() && ticket.StateExpiresAt.Before(ticket.ExpiresAt) {
			ticket.ExpiresAt = ticket.StateExpiresAt
		}
		if cfg.CredentialMode == config.CodexTicketCredentialCookieState && ticket.StateExpiresAt.IsZero() {
			ticket.ExpiresAt = earlierCodexTicketExpiry(ticket.ExpiresAt, captured.Add(ttl))
		}
		if !ticket.StateExpiresAt.IsZero() && ticket.StateExpiresAt.Before(ticket.RevalidateAt) {
			ticket.RevalidateAt = ticket.StateExpiresAt
		}
		if cfg.CredentialMode == config.CodexTicketCredentialCookie {
			ticket.State, ticket.Length = "", 0
			ticket.IssuedAt, ticket.StateExpiresAt = time.Time{}, time.Time{}
		}
	}
	// 新采集的票是谱系起点；之后的软复验会刷新 CapturedAt，但谱系时间保持不变。
	ticket.OriginCapturedAt = ticket.CapturedAt
	attempt.Reason = "publish_failed"
	if s.storeOpenAICodexTicket(ctx, input.Account, ticket) {
		attempt.Success, attempt.Reason = true, "verified"
		if !verified {
			attempt.Reason = "verification_skipped"
		}
		attempt.TicketCapturedAt, attempt.TicketExpiresAt = &ticket.CapturedAt, &ticket.ExpiresAt
		s.reportOpenAICodexTicketProxyResult(ctx, proxy, true)
		if verified {
			ReportRandomProxySuccess(ctx, input.Account, s.accountRepo)
		}
		fields := []zap.Field{zap.Int64("account_id", account.ID), zap.String("model", model), zap.Bool("business_verified", verified)}
		// 只记录 __oailb 解码后的声明与到期时间，不记录原始 Cookie。
		if claims := codexOAILBClaimsForLog(ticket); claims != nil {
			fields = append(fields, zap.Any("oailb_claims", claims))
		}
		if routeExpires := codexTicketRouteExpiresAt(ticket); !routeExpires.IsZero() {
			fields = append(fields, zap.Time("route_expires_at", routeExpires))
		}
		logger.L().Info("openai_codex_ticket published", fields...)
	}
}

func (s *OpenAIGatewayService) reloadOpenAICodexTicketProbeAccount(ctx context.Context, account *Account) *Account {
	if !s.openAICodexTicketProbeAllowed(ctx, account, "") {
		return nil
	}
	if repo, ok := s.accountRepo.(codexTicketAccountReloader); ok {
		latest, err := repo.GetCodexTicketAccountSnapshot(ctx, account.ID)
		if err != nil || latest == nil || latest.ID != account.ID {
			logger.L().Debug("openai_codex_ticket probe skipped", zap.Int64("account_id", account.ID), zap.String("reason", "account_snapshot_unavailable"))
			return nil
		}
		account = latest
	}
	if !s.openAICodexTicketProbeAllowed(ctx, account, "") {
		return nil
	}
	return cloneOpenAICodexTicketAccount(account)
}

func (s *OpenAIGatewayService) openAICodexTicketProbeAllowed(ctx context.Context, account *Account, token string) bool {
	return s != nil && ctx.Err() == nil && openAICodexTicketHarvestEnabled(account) &&
		(!account.IsInDailyCooldown(time.Now()) || isCodexTicketManualRetry(ctx)) && s.openAICodexTicketEnabledContext(ctx) &&
		(token == "" || isCodexTicketManualRetry(ctx) || s.usesSharedCodexProtection(ctx) || !s.openAICodexTicketCooling(account, token))
}

func openAICodexTicketHarvestEnabled(account *Account) bool {
	if account == nil || account.Status != StatusActive || !account.Schedulable ||
		!SharedPoolSharingAllowed(account) || !OpenAICodexTicketAccountEnabled(account) {
		return false
	}
	// 人工停用必须停止采集；临时业务限流不阻断后续恢复所需的打票。
	return !account.AutoPauseOnExpired || account.ExpiresAt == nil || time.Now().Before(*account.ExpiresAt)
}
