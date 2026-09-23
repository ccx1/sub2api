package service

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func codexTicketQualityEnabled(in openAICodexTicketProbeInput) bool {
	return !in.Revalidation && in.Config != nil && config.CodexTicketBusinessVerificationEnabled(*in.Config) &&
		in.Config.BusinessVerificationRounds > 1
}

func (s *OpenAIGatewayService) verifyOpenAICodexTicketBusiness(ctx context.Context, input *openAICodexTicketProbeInput, state string) bool {
	if !s.prepareCodexTicketBusiness(ctx, input, state) {
		return false
	}
	if input.Revalidation && input.Attempt != nil {
		input.Attempt.BusinessVerificationRounds = 1
	}
	quality := codexTicketQualityEnabled(*input)
	if quality && !s.verifyCodexTicketQuality(ctx, input) {
		return false
	}
	if schedule := codexTicketScheduleFrom(ctx); schedule != nil {
		ctx = WithCodexTicketBusinessReservation(ctx, schedule.reservation)
	}
	harvestURL := input.ProxyURL
	if !s.resolveCodexTicketBusinessProxy(ctx, input) {
		return false
	}
	if !input.Revalidation && !config.CodexTicketBusinessVerificationEnabled(*input.Config) {
		return true
	}
	if quality && input.ProxyURL == harvestURL {
		if input.Attempt != nil {
			var id int64
			var name string
			if input.Account.Proxy != nil {
				id, name = input.Account.Proxy.ID, input.Account.Proxy.Name
			}
			input.Attempt.BusinessProxy = codexTicketProxySnapshot(input.ProxyURL, id, name)
		}
		// 连续探测已验证同一出口，补齐业务准入后即可发布，无需多发一轮。
		if err := s.admitCodexTicketProbe(ctx, *input); err != nil {
			s.recordCodexTicketBusinessFailure(ctx, input, 0, err)
			return false
		}
		recordCodexTicketBusinessProxyResult(ctx, http.StatusOK, nil, false)
		return true
	}
	return s.runCodexTicketBusinessProbe(ctx, input)
}

func (s *OpenAIGatewayService) prepareCodexTicketBusiness(ctx context.Context, input *openAICodexTicketProbeInput, state string) bool {
	if input.Config == nil {
		cfg := s.openAICodexTicketConfigForAccount(ctx, input.Account)
		input.Config, input.SubscriptionTier = &cfg, openAICodexTicketSubscriptionTier(input.Account)
	}
	if !s.openAICodexTicketProbeConfigCurrent(ctx, *input) {
		return false
	}
	account := s.reloadOpenAICodexTicketProbeAccount(ctx, input.Account)
	if account == nil || s.codexTicketBindingForConfig(account, *input.Config) != s.codexTicketBindingForConfig(input.Account, *input.Config) ||
		openAICodexTicketSubscriptionTier(account) != input.SubscriptionTier ||
		account.GetCredential("access_token") != input.Account.GetCredential("access_token") ||
		account.GetCredential("refresh_token") != input.Account.GetCredential("refresh_token") {
		return false
	}
	current := *input
	current.Account = account
	if !s.openAICodexTicketProbeConfigCurrent(ctx, current) || !s.codexTicketProxyPolicyCurrent(ctx, current) {
		return false
	}
	input.Account, input.State = account, state
	input.BusinessVerification, input.CheckControls = true, true
	if input.Config.CredentialMode == config.CodexTicketCredentialCookie {
		input.State = ""
	}
	return true
}

func (s *OpenAIGatewayService) resolveCodexTicketBusinessProxy(ctx context.Context, input *openAICodexTicketProbeInput) bool {
	account := input.Account
	if err := ResolveRandomProxyFromSource(ctx, account, s.accountRepo); err != nil {
		var wait *CodexTicketWaitError
		if schedule := codexTicketScheduleFrom(ctx); schedule != nil && errors.As(err, &wait) {
			schedule.deferred = wait.Status
			if input.Attempt != nil {
				input.Attempt.Reason = "verification_deferred"
			}
			return false
		}
		if input.Attempt != nil {
			input.Attempt.Reason = "business_proxy_unavailable"
		}
		_ = DisableRandomProxyAccountOnUnavailable(ctx, account, s.accountRepo, err)
		return false
	}
	if account.ProxyID != nil && account.Proxy == nil {
		if input.Attempt != nil {
			input.Attempt.Reason = "business_proxy_unavailable"
		}
		return false
	}
	input.ProxyURL = resolveAccountProxyURL(account)
	return true
}

func (s *OpenAIGatewayService) verifyCodexTicketQuality(ctx context.Context, input *openAICodexTicketProbeInput) bool {
	input.FreezeCredentials, input.QualityVerification = true, true
	defer func() { input.QualityVerification = false }()
	if input.BusinessCredentialSnapshot == nil {
		input.BusinessCredentialSnapshot = codexTicketBusinessCookieSnapshot(*input)
	}
	rounds := input.Config.BusinessVerificationRounds
	if input.Attempt != nil {
		input.Attempt.BusinessVerificationRounds = rounds
		input.Attempt.BusinessVerificationPassed = 0
		input.Attempt.BusinessVerificationModels = nil
	}
	for round := 0; round < rounds; round++ {
		if !s.runCodexTicketBusinessProbe(ctx, input) {
			return false
		}
	}
	return true
}

func (s *OpenAIGatewayService) runCodexTicketBusinessProbe(ctx context.Context, input *openAICodexTicketProbeInput) bool {
	returned, status, err := s.probeOpenAICodexTicket(ctx, *input)
	rejected := (!config.CodexTicketUsesCookies(*input.Config) || input.FreezeCredentials) && codexTicketStateRejected(returned, *input.Config)
	if err == nil && (status != http.StatusOK || rejected) {
		err = &codexTicketProbeResponseError{reason: "ticket_rejected"}
	}
	recordCodexTicketVerificationResult(ctx, input, status, err)
	if err != nil {
		s.recordCodexTicketBusinessFailure(ctx, input, status, err)
		return false
	}
	return true
}

func recordCodexTicketVerificationResult(ctx context.Context, input *openAICodexTicketProbeInput, status int, err error) {
	if !input.QualityVerification {
		recordCodexTicketBusinessProxyResult(ctx, status, err, false)
	}
	if schedule := codexTicketScheduleFrom(ctx); schedule != nil && input.QualityVerification {
		schedule.qualityProxyFailed = codexTicketProxyAttemptFailed(ctx, status, err, false)
	}
	attempt := input.Attempt
	if attempt == nil {
		return
	}
	if attempt.BusinessExchange != nil {
		attempt.BusinessVerificationModels = appendUniqueStrings(attempt.BusinessVerificationModels, attempt.BusinessExchange.ReportedModels...)
	}
	if err == nil && input.QualityVerification {
		attempt.BusinessVerificationPassed++
	} else if err == nil && !input.FreezeCredentials {
		attempt.BusinessVerificationPassed = 1
	}
}

func (s *OpenAIGatewayService) recordCodexTicketBusinessFailure(ctx context.Context, input *openAICodexTicketProbeInput, status int, err error) {
	if schedule := codexTicketScheduleFrom(ctx); schedule != nil {
		schedule.upstream = codexTicketProbeUpstreamError(err)
		if schedule.deferred != nil {
			return
		}
	}
	if input.Attempt != nil {
		input.Attempt.Reason = codexTicketAttemptErrorReason("business", err)
		input.Attempt.canceled = errors.Is(err, context.Canceled)
		if errors.Is(err, errOpenAICodexTicketControlsChanged) {
			input.Attempt.Reason = "controls_changed"
		}
	}
	if !codexTicketScheduledProtected(ctx) {
		s.coolOpenAICodexTicket(input.Account, input.Token, err)
	}
	if codexTicketScheduleFrom(ctx) != nil || !codexTicketProxyAttemptFailed(ctx, status, err, false) {
		return
	}
	if input.QualityVerification {
		if input.HarvestProxy != nil {
			s.reportOpenAICodexTicketProxyResult(ctx, *input.HarvestProxy, false)
		}
		return
	}
	var rejected *openAICodexTicketProbeRejected
	var transport *codexTicketTransportError
	if errors.As(err, &rejected) {
		if rejected.Status >= 500 || rejected.Status == http.StatusProxyAuthRequired {
			reportRandomProxyResult(ctx, input.Account, s.accountRepo, false)
		}
	} else if errors.As(err, &transport) {
		ReportRandomProxyTransportFailure(ctx, input.Account, s.accountRepo, err)
	} else if status > 0 && s.openAICodexTicketProbeAllowed(ctx, input.Account, input.Token) {
		reportRandomProxyResult(ctx, input.Account, s.accountRepo, false)
	}
}

func appendUniqueStrings(dst []string, values ...string) []string {
	for _, value := range values {
		if value != "" && !slices.Contains(dst, value) {
			dst = append(dst, value)
		}
	}
	return dst
}
