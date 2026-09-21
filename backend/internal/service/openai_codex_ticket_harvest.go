package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

type codexTicketAccountReloader interface {
	GetCodexTicketAccountSnapshot(context.Context, int64) (*Account, error)
}

// 候选票只有完整响应模型匹配，并在同账号业务出口复验通过后才能发布。
func (s *OpenAIGatewayService) probeOnceOpenAICodexTicket(ctx context.Context, account *Account, model string) {
	if s == nil || s.httpUpstream == nil || !OpenAICodexTicketAccountEnabled(account) || ctx.Err() != nil || !s.openAICodexTicketEnabledContext(ctx) {
		return
	}
	key := openAICodexTicketKey(account.ID, model)
	_, _, _ = s.openaiCodexTicketFlight.Do(key, func() (any, error) {
		s.harvestVerifiedOpenAICodexTicket(ctx, cloneOpenAICodexTicketAccount(account), model)
		return nil, nil
	})
}

func (s *OpenAIGatewayService) harvestVerifiedOpenAICodexTicket(ctx context.Context, account *Account, model string) {
	account = s.reloadOpenAICodexTicketProbeAccount(ctx, account)
	if account == nil {
		return
	}
	cfg := s.openAICodexTicketConfigContext(ctx)
	if !s.openAICodexTicketGatedModelContext(ctx, model) {
		return
	}
	now := time.Now()
	current := s.lookupOpenAICodexTicket(account, model)
	// 开关恢复和已排队任务都复查当前票，避免固定出口重复采集仍有效的旧票。
	if current.usable(now, account, cfg) && (!current.needsRefresh(now, time.Duration(cfg.RefreshBeforeSeconds)*time.Second) ||
		!isCodexTicketManualRetry(ctx) && s.keepUsableCodexTicketForProxy(ctx, account)) {
		return
	}
	token, _, err := s.GetAccessToken(ctx, account)
	if err != nil || strings.TrimSpace(token) == "" {
		return
	}
	if s.openAICodexTicketCooling(account, token) || (!isCodexTicketManualRetry(ctx) && s.openAICodexTicketBackoffActive(account, token, model)) {
		return
	}
	proxy, err := s.selectOpenAICodexTicketProxy(ctx, account)
	if err != nil || (proxy.url == "" && proxy.policy.mode != CodexTicketProxyModeAccount) {
		return
	}
	binding, captured := s.codexTicketBindingForConfig(account, cfg), time.Now()
	attempt := newCodexTicketAttempt(model)
	captureCodexTicketAttemptPolicy(attempt, account, cfg)
	attempt.HarvestProxy = codexTicketProxySnapshot(proxy.url, proxy.proxyID, proxy.proxyName)
	input := openAICodexTicketProbeInput{Account: account, Token: token, Model: model, ProxyURL: proxy.url,
		Timeout: time.Duration(cfg.HarvestAttemptTimeoutSeconds) * time.Second, CheckControls: true, Attempt: attempt,
		Config: &cfg, SubscriptionTier: openAICodexTicketSubscriptionTier(account), HarvestProxyPolicy: &proxy.policy}
	defer s.finishOpenAICodexTicketHarvest(ctx, &input)
	state, status, err := s.probeOpenAICodexTicket(ctx, input)
	if err != nil {
		attempt.canceled = errors.Is(err, context.Canceled)
		if errors.Is(err, errOpenAICodexTicketControlsChanged) {
			attempt.Reason = "controls_changed"
			return
		}
		s.coolOpenAICodexTicket(account, token, err)
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
	if status != http.StatusOK || !codexTicketCandidateAccepted(state, account, cfg) {
		attempt.Reason = codexTicketCandidateFailureReason(state, account, cfg)
		s.reportOpenAICodexTicketProxyResult(ctx, proxy, false)
		return
	}
	attempt.Reason = "controls_changed"
	if !s.verifyOpenAICodexTicketBusiness(ctx, &input, state) {
		return
	}
	if !s.openAICodexTicketProbeAllowed(ctx, input.Account, input.Token) || !s.openAICodexTicketProbeConfigCurrent(ctx, input) ||
		binding != s.codexTicketBindingForConfig(input.Account, s.openAICodexTicketConfigContext(ctx)) || !s.codexTicketAccountCurrentBeforePublish(ctx, input) {
		return
	}
	ticket := &openAICodexTicket{AccountID: account.ID, Model: model, State: state, Length: len(state), CapturedAt: captured,
		ExpiresAt: captured.Add(time.Duration(cfg.TTLSeconds) * time.Second), Attempts: 1, Verified: true, Binding: binding,
		Egress: openAICodexTicketEgress(input.ProxyURL)}
	attempt.Reason = "publish_failed"
	if s.storeOpenAICodexTicket(ctx, input.Account, ticket) {
		attempt.Success, attempt.Reason = true, "verified"
		s.reportOpenAICodexTicketProxyResult(ctx, proxy, true)
		ReportRandomProxySuccess(ctx, input.Account, s.accountRepo)
		logger.L().Info("openai_codex_ticket verified", zap.Int64("account_id", account.ID), zap.String("model", model))
	}
}

func (s *OpenAIGatewayService) verifyOpenAICodexTicketBusiness(ctx context.Context, input *openAICodexTicketProbeInput, state string) bool {
	if input.Config == nil {
		cfg := s.openAICodexTicketConfigContext(ctx)
		input.Config, input.SubscriptionTier = &cfg, openAICodexTicketSubscriptionTier(input.Account)
	}
	if !s.openAICodexTicketProbeConfigCurrent(ctx, *input) {
		return false
	}
	account := s.reloadOpenAICodexTicketProbeAccount(ctx, input.Account)
	if account == nil {
		return false
	}
	if s.codexTicketBindingForConfig(account, *input.Config) != s.codexTicketBindingForConfig(input.Account, *input.Config) ||
		openAICodexTicketSubscriptionTier(account) != input.SubscriptionTier ||
		account.GetCredential("access_token") != input.Account.GetCredential("access_token") ||
		account.GetCredential("refresh_token") != input.Account.GetCredential("refresh_token") {
		logger.L().Debug("openai_codex_ticket probe skipped", zap.Int64("account_id", account.ID), zap.String("reason", "account_identity_changed"))
		return false
	}
	currentInput := *input
	currentInput.Account = account
	if !s.codexTicketProxyPolicyCurrent(ctx, currentInput) {
		return false
	}
	if err := ResolveRandomProxyFromSource(ctx, account, s.accountRepo); err != nil {
		if input.Attempt != nil {
			input.Attempt.Reason = "business_proxy_unavailable"
		}
		_ = DisableRandomProxyAccountOnUnavailable(ctx, account, s.accountRepo, err)
		return false
	}
	// 固定代理未加载时禁止把缺失关联误判成直连。
	if account.ProxyID != nil && account.Proxy == nil {
		if input.Attempt != nil {
			input.Attempt.Reason = "business_proxy_unavailable"
		}
		return false
	}
	input.Account, input.ProxyURL, input.State = account, resolveAccountProxyURL(account), state
	input.CheckControls = true
	returned, status, err := s.probeOpenAICodexTicket(ctx, *input)
	if err != nil {
		if input.Attempt != nil {
			input.Attempt.Reason = codexTicketAttemptErrorReason("business", err)
			input.Attempt.canceled = errors.Is(err, context.Canceled)
			if errors.Is(err, errOpenAICodexTicketControlsChanged) {
				input.Attempt.Reason = "controls_changed"
			}
		}
		s.coolOpenAICodexTicket(account, input.Token, err)
		var rejected *openAICodexTicketProbeRejected
		var transport *codexTicketTransportError
		if errors.As(err, &rejected) {
			if rejected.Status >= 500 || rejected.Status == http.StatusProxyAuthRequired {
				reportRandomProxyResult(ctx, account, s.accountRepo, false)
			}
		} else if errors.As(err, &transport) {
			ReportRandomProxyTransportFailure(ctx, account, s.accountRepo, err)
		} else if status > 0 && s.openAICodexTicketProbeAllowed(ctx, account, input.Token) {
			reportRandomProxyResult(ctx, account, s.accountRepo, false)
		}
		return false
	}
	if status != http.StatusOK || codexTicketStateRejected(returned, *input.Config) {
		if input.Attempt != nil {
			input.Attempt.Reason = "business_ticket_rejected"
		}
		reportRandomProxyResult(ctx, account, s.accountRepo, false)
		return false
	}
	return true
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
		// 不使用 schedulable 或临时限流状态，保留恢复采集能力。
		if latest.Status != StatusActive || latest.AutoPauseOnExpired && latest.ExpiresAt != nil && !time.Now().Before(*latest.ExpiresAt) {
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
	return s != nil && ctx.Err() == nil && OpenAICodexTicketAccountEnabled(account) &&
		!account.IsInDailyCooldown(time.Now()) && s.openAICodexTicketEnabledContext(ctx) &&
		(token == "" || !s.openAICodexTicketCooling(account, token))
}
