package service

import (
	"context"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type codexTicketScheduleKey struct{}
type codexTicketProxyResolutionKey struct{}

func WithCodexTicketProxyResolution(ctx context.Context) context.Context {
	return context.WithValue(ctx, codexTicketProxyResolutionKey{}, true)
}

func IsCodexTicketProxyResolution(ctx context.Context) bool {
	value, _ := ctx.Value(codexTicketProxyResolutionKey{}).(bool)
	return value
}

type codexTicketSchedule struct {
	scheduler                  CodexTicketScheduler
	reservation                *CodexTicketReservation
	started, accepted, silence bool
	upstream                   *CodexTicketUpstreamError
	deferred                   *CodexTicketRuntimeStatus
	harvestProxyFailed         bool
	businessProxyFailed        bool
	businessProxySucceeded     bool
	qualityProxyFailed         bool
}

func codexTicketScheduleFrom(ctx context.Context) *codexTicketSchedule {
	value, _ := ctx.Value(codexTicketScheduleKey{}).(*codexTicketSchedule)
	return value
}

func (s *OpenAIGatewayService) reserveCodexTicketHarvest(ctx context.Context, account *Account, model string, cfg config.OpenAICodexTicketConfig) (context.Context, openAICodexTicketProxy, error) {
	ctx = WithCodexTicketProxyResolution(ctx)
	scheduler, ok := s.accountRepo.(CodexTicketScheduler)
	if !ok {
		if err := s.checkCodexTicketBusinessIdle(ctx, account); err != nil {
			return ctx, openAICodexTicketProxy{}, err
		}
		if cfg.TicketProtection().Enabled || cfg.TicketProtection().ProxyIPProtectionEnabled || account.CodexTicketProxyStrategy() == CodexTicketProxyStrategyRoundRobin {
			return ctx, openAICodexTicketProxy{}, errors.New("codex ticket shared scheduler unavailable")
		}
		proxy, err := s.selectOpenAICodexTicketProxy(ctx, account)
		return ctx, proxy, err
	}
	policy, err := s.scheduledCodexTicketProxyPolicy(ctx, account)
	if err != nil {
		return ctx, openAICodexTicketProxy{}, err
	}
	if cfg.TicketProtection().ProxyIPProtectionEnabled && policy.mode == CodexTicketProxyModeFixed && policy.proxyID == 0 && policy.url != "" {
		return ctx, openAICodexTicketProxy{}, errors.New("codex ticket IP protection requires a managed harvest proxy; add and select the proxy in proxy management")
	}
	// 共享调度按全局配置做版本围栏，账号 Cookie 寿命只用于探测与票据有效性。
	if policy.followBusiness && !account.IsRandomProxy() {
		businessProxy, resolveErr := s.resolveCodexTicketAccountProxy(ctx, account)
		if resolveErr != nil {
			return ctx, openAICodexTicketProxy{}, resolveErr
		}
		policy.proxyID, policy.url = businessProxy.proxyID, businessProxy.url
		policy.followBusiness = true
	}
	businessPool := policy.followBusiness && account.IsRandomProxy()
	allowDirectOnEmpty := policy.followBusiness && account.IsRandomProxy() && account.RandomProxyEmptyPoolPolicy() == RandomProxyEmptyPoolPolicyDirect
	request := CodexTicketReserveRequest{AccountID: account.ID, Model: model, Config: s.openAICodexTicketConfigContext(ctx),
		Models: s.codexTicketPendingModels(ctx, account, cfg), Strategy: policy.strategy, Manual: isCodexTicketManualRetry(ctx),
		Selection: ProxyPoolSelection{AccountID: account.ID, CountryCode: policy.countryCode}, PoolMode: policy.mode == "pool" || policy.mode == CodexTicketProxyModeRandom || businessPool,
		HarvestUsesBusiness: policy.followBusiness, AllowDirectOnEmpty: allowDirectOnEmpty}
	proxy := openAICodexTicketProxy{url: policy.url, proxyID: policy.proxyID, policy: policy}
	if request.PoolMode && policy.inherited && account.IsRandomProxy() {
		request.Selection, err = ResolveAccountProxyPoolSelection(ctx, account, s.accountRepo)
		if err != nil {
			return ctx, proxy, err
		}
	}
	if !request.PoolMode && policy.proxyID > 0 {
		loader, available := s.accountRepo.(codexTicketProxyLoader)
		if !available {
			return ctx, proxy, ErrRandomProxyUnavailable
		}
		request.FixedProxy, err = loader.GetCodexTicketProxy(ctx, policy.proxyID)
		if err != nil || !codexTicketProxyAvailable(request.FixedProxy) || request.FixedProxy.URL() != policy.url {
			return ctx, proxy, ErrRandomProxyUnavailable
		}
		if err := validateProxyRegion(ctx, request.FixedProxy, policy.countryCode, s.accountRepo); err != nil {
			return ctx, proxy, err
		}
	}
	// 跟随账号的随机空池直连仍遵循管理员显式选择的 direct 策略。
	if !request.PoolMode && policy.mode == CodexTicketProxyModeAccount && policy.proxyID == 0 &&
		(account.IsRandomProxy() && account.RandomProxyEmptyPoolPolicy() == RandomProxyEmptyPoolPolicyDirect || !account.IsRandomProxy()) {
		request.Selection.CountryCode = ""
	}
	reservation, err := s.reserveCodexTicketWithManualWait(ctx, scheduler, request)
	if err != nil {
		if policy.followBusiness {
			err = s.codexTicketEmptyPoolError(ctx, account, err)
		}
		return ctx, proxy, err
	}
	if reservation == nil {
		return ctx, proxy, errors.New("codex ticket reservation unavailable")
	}
	if reservation.Proxy != nil {
		if !reservation.Proxy.RegionFallback {
			if err := validateProxyRegion(ctx, reservation.Proxy, policy.countryCode, s.accountRepo); err != nil {
				_ = scheduler.FinishCodexTicket(ctx, CodexTicketFinishRequest{Reservation: reservation, Outcome: "canceled"})
				return ctx, proxy, err
			}
		}
		proxy.url, proxy.proxyID, proxy.proxyName = reservation.Proxy.URL(), reservation.Proxy.ID, reservation.Proxy.Name
	} else if request.Selection.CountryCode != "" && !request.AllowDirectOnEmpty {
		_ = scheduler.FinishCodexTicket(ctx, CodexTicketFinishRequest{Reservation: reservation, Outcome: "canceled"})
		return ctx, proxy, errors.New("region-restricted ticket reservation returned no proxy")
	}
	return context.WithValue(ctx, codexTicketScheduleKey{}, &codexTicketSchedule{scheduler: scheduler, reservation: reservation}), proxy, nil
}

func (s *OpenAIGatewayService) reserveCodexTicketWithManualWait(ctx context.Context, scheduler CodexTicketScheduler, request CodexTicketReserveRequest) (*CodexTicketReservation, error) {
	for {
		reservation, err := scheduler.ReserveCodexTicket(ctx, request)
		if err == nil || !request.Manual {
			return reservation, err
		}
		var wait *CodexTicketWaitError
		if !errors.As(err, &wait) || wait == nil || wait.Status == nil || !codexTicketManualWaitRecoverable(wait.Status.Reason) {
			return reservation, err
		}
		delay := 100 * time.Millisecond
		if wait.Status.RetryAt != nil {
			if until := time.Until(*wait.Status.RetryAt); until > delay {
				delay = min(until, time.Second)
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func codexTicketManualWaitRecoverable(reason string) bool {
	switch reason {
	case "account_busy", "business_active", "capacity", "half_open_busy", "proxy_silent", "proxy_unhealthy", "proxy_switch_waiting", "recovery_waiting_for_harvest":
		return true
	default:
		return false
	}
}

func (s *OpenAIGatewayService) codexTicketPendingModels(ctx context.Context, account *Account, cfg config.OpenAICodexTicketConfig) []string {
	models, _ := codexTicketRetryModels(cfg, "")
	pending := make([]string, 0, len(models))
	for _, model := range models {
		if s.codexModelQualityCircuitPaused(ctx, account, model) {
			continue
		}
		if !isCodexTicketManualRetry(ctx) && !s.codexTicketInventoryNeedsRefresh(account, model, cfg, time.Now()) {
			continue
		}
		pending = append(pending, model)
	}
	return pending
}

func (s *OpenAIGatewayService) admitCodexTicketProbe(ctx context.Context, in openAICodexTicketProbeInput) error {
	schedule := codexTicketScheduleFrom(ctx)
	if schedule == nil {
		return nil
	}
	if in.QualityVerification || in.SkipSchedulerAdmission {
		// 重复请求仍检查租约；共享存储不可用属于围栏失败，不能惩罚代理。
		if err := schedule.scheduler.ValidateCodexTicket(ctx, schedule.reservation); err != nil {
			return errOpenAICodexTicketControlsChanged
		}
		return nil
	}
	if !codexTicketProbeBusiness(in) {
		if err := schedule.scheduler.StartCodexTicket(ctx, schedule.reservation); err != nil {
			return err
		}
		schedule.started = true
		return nil
	}
	proxy := in.Account.Proxy
	err := schedule.scheduler.CheckCodexTicketStage(ctx, schedule.reservation, proxy)
	if err != nil {
		var wait *CodexTicketWaitError
		if errors.As(err, &wait) && wait.Status != nil {
			schedule.deferred = wait.Status
		} else {
			schedule.deferred = &CodexTicketRuntimeStatus{State: "waiting", Reason: "shared_state_unavailable"}
		}
		if proxy != nil {
			schedule.deferred.ProxyID = proxy.ID
		}
		if in.Attempt != nil {
			in.Attempt.Reason = "verification_deferred"
		}
	}
	return err
}

// GET 只读共享状态，不抢租约、不推进半开或预算。
func (s *OpenAIGatewayService) GetOpenAICodexTicketRuntimeStatus(ctx context.Context, accountID int64) (*CodexTicketRuntimeStatus, error) {
	if s == nil {
		return nil, ErrCodexTicketRetryUnavailable
	}
	scheduler, ok := s.accountRepo.(CodexTicketScheduler)
	if !ok {
		return &CodexTicketRuntimeStatus{State: "unavailable", Reason: "shared_state_unavailable"}, nil
	}
	status, err := scheduler.GetCodexTicketRuntimeStatus(ctx, accountID)
	if err != nil {
		return &CodexTicketRuntimeStatus{State: "unavailable", Reason: "shared_state_unavailable"}, nil
	}
	return status, nil
}
