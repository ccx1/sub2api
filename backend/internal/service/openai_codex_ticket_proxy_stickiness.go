package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

type codexTicketBusinessReservationKey struct{}

func (s *OpenAIGatewayService) scheduledCodexTicketProxyPolicy(ctx context.Context, account *Account) (codexTicketProxyPolicy, error) {
	if account == nil {
		return s.codexTicketProxyPolicy(ctx, account)
	}
	if !account.IsRandomProxy() || account.CodexTicketProxyMode() != CodexTicketProxyModeAccount {
		policy, err := s.codexTicketProxyPolicy(ctx, account)
		if err != nil || policy.mode != CodexTicketProxyModeAccount || !policy.followBusiness {
			return policy, err
		}
		if !account.IsRandomProxy() {
			businessProxy, err := s.resolveCodexTicketAccountProxy(ctx, account)
			if err != nil {
				return policy, err
			}
			policy.proxyID, policy.url = businessProxy.proxyID, businessProxy.url
		}
		return policy, nil
	}
	if err := ValidateCodexTicketProxyExtra(account.Extra); err != nil {
		return codexTicketProxyPolicy{}, err
	}
	country, err := account.ProxyRegionCountry()
	return codexTicketProxyPolicy{mode: CodexTicketProxyModeAccount, strategy: account.CodexTicketProxyStrategy(),
		countryCode: country, inherited: true, followBusiness: true}, err
}

func (s *OpenAIGatewayService) codexTicketEmptyPoolError(ctx context.Context, account *Account, cause error) error {
	var wait *CodexTicketWaitError
	if !errors.As(cause, &wait) || wait.Status == nil || wait.Status.Reason != "pool_empty" {
		return cause
	}
	// 仓储停用入口重新核验当前池和账号策略，临时静默或容量等待不能停用。
	if err := DisableRandomProxyAccountOnUnavailable(ctx, account, s.accountRepo, randomProxyUnavailable(account)); err != nil {
		return fmt.Errorf("%w; disable random proxy account: %v", cause, err)
	}
	return cause
}

func WithCodexTicketBusinessReservation(ctx context.Context, reservation *CodexTicketReservation) context.Context {
	return context.WithValue(ctx, codexTicketBusinessReservationKey{}, reservation)
}

func CodexTicketBusinessReservationFrom(ctx context.Context) *CodexTicketReservation {
	reservation, _ := ctx.Value(codexTicketBusinessReservationKey{}).(*CodexTicketReservation)
	return reservation
}

// 只有实际请求失败才影响出口粘性；账号冷却、配置变化和等待不是代理失败。
func codexTicketProxyAttemptFailed(ctx context.Context, status int, cause error, rejected bool) bool {
	if ctx.Err() != nil || errors.Is(cause, context.Canceled) || errors.Is(cause, errOpenAICodexTicketControlsChanged) {
		return false
	}
	var wait *CodexTicketWaitError
	if errors.As(cause, &wait) {
		return false
	}
	if detail := codexTicketProbeUpstreamError(cause); detail != nil {
		status = detail.EffectiveStatus
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusTooManyRequests {
		return false
	}
	return cause != nil || rejected || status != http.StatusOK
}

func recordCodexTicketBusinessProxyResult(ctx context.Context, status int, cause error, rejected bool) {
	schedule := codexTicketScheduleFrom(ctx)
	if schedule == nil {
		return
	}
	schedule.businessProxyFailed = codexTicketProxyAttemptFailed(ctx, status, cause, rejected)
	schedule.businessProxySucceeded = ctx.Err() == nil && cause == nil && status == http.StatusOK && !rejected
}
