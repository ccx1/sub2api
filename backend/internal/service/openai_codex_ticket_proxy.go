package service

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"
)

type openAICodexTicketProxy struct {
	url       string
	accountID int64
	proxyID   int64
	proxyName string
	policy    codexTicketProxyPolicy
}

type codexTicketProxyPolicy struct {
	strategy       string
	mode           string
	proxyID        int64
	url            string
	inherited      bool
	countryCode    string
	followBusiness bool
}

type codexTicketProxyLoader interface {
	GetCodexTicketProxy(context.Context, int64) (*Proxy, error)
}

type codexTicketTransportError struct{ error }

func (e *codexTicketTransportError) Unwrap() error { return e.error }

func (s *OpenAIGatewayService) selectOpenAICodexTicketProxy(ctx context.Context, account *Account) (openAICodexTicketProxy, error) {
	if account == nil || account.IsInDailyCooldown(time.Now()) {
		return openAICodexTicketProxy{}, errors.New("account is unavailable during daily cooldown")
	}
	if account.CodexTicketProxyMode() == CodexTicketProxyModeAccount {
		return s.resolveCodexTicketAccountProxy(ctx, account)
	}
	policy, err := s.codexTicketProxyPolicy(ctx, account)
	if err != nil {
		return openAICodexTicketProxy{}, err
	}
	if policy.mode == CodexTicketProxyModeAccount {
		proxy, err := s.resolveCodexTicketAccountProxy(ctx, account)
		if err != nil {
			return openAICodexTicketProxy{}, err
		}
		// 全局 account 模式仍需保留全局策略快照，发布前会用它核对
		// 全局设置是否发生变化。固定/直连业务出口只回填实际出口，
		// 随机账号继续保留未解析的账号池策略。
		proxy.policy = policy
		if !account.IsRandomProxy() {
			proxy.policy.proxyID, proxy.policy.url = proxy.proxyID, proxy.url
		}
		return proxy, nil
	}
	if policy.mode == "fixed" {
		proxy := openAICodexTicketProxy{url: policy.url, proxyID: policy.proxyID, policy: policy}
		if policy.proxyID > 0 {
			loader, ok := s.accountRepo.(codexTicketProxyLoader)
			if !ok {
				return openAICodexTicketProxy{}, errors.New("codex ticket proxy loader is unavailable")
			}
			loaded, err := loader.GetCodexTicketProxy(ctx, policy.proxyID)
			if err != nil || !codexTicketProxyAvailable(loaded) || loaded.URL() != policy.url {
				return openAICodexTicketProxy{}, ErrRandomProxyUnavailable
			}
			if err := validateProxyRegion(ctx, loaded, policy.countryCode, s.accountRepo); err != nil {
				return openAICodexTicketProxy{}, err
			}
			proxy.proxyName = loaded.Name
		}
		return proxy, nil
	}
	selector, ok := s.accountRepo.(BalancedProxySelector)
	if !ok {
		return openAICodexTicketProxy{}, errors.New("codex ticket proxy pool is not configured")
	}
	selection := ProxyPoolSelection{AccountID: account.ID, CountryCode: policy.countryCode}
	if policy.inherited && account.IsRandomProxy() {
		selection, err = ResolveAccountProxyPoolSelection(ctx, account, s.accountRepo)
		if err != nil {
			return openAICodexTicketProxy{}, err
		}
	}
	proxy, err := selector.SelectBalancedProxy(ctx, selection)
	if err != nil {
		return openAICodexTicketProxy{}, err
	}
	if !codexTicketProxyAvailable(proxy) || (selection.Restricted && !slices.Contains(selection.IDs, proxy.ID)) {
		return openAICodexTicketProxy{}, ErrRandomProxyUnavailable
	}
	if !proxy.RegionFallback {
		if err := validateProxyRegion(ctx, proxy, policy.countryCode, s.accountRepo); err != nil {
			return openAICodexTicketProxy{}, err
		}
	}
	return openAICodexTicketProxy{url: proxy.URL(), accountID: account.ID, proxyID: proxy.ID, proxyName: proxy.Name, policy: policy}, nil
}

func (s *OpenAIGatewayService) codexTicketProxyPolicy(ctx context.Context, account *Account) (codexTicketProxyPolicy, error) {
	if account == nil || ValidateCodexTicketProxyExtra(account.Extra) != nil {
		return codexTicketProxyPolicy{}, errors.New("invalid codex ticket proxy settings")
	}
	policy := codexTicketProxyPolicy{mode: account.CodexTicketProxyMode(), proxyID: account.CodexTicketProxyID(), strategy: account.CodexTicketProxyStrategy()}
	var err error
	policy.countryCode, err = account.ProxyRegionCountry()
	if err != nil {
		return policy, err
	}
	if policy.mode == CodexTicketProxyModeAccount {
		proxy, err := s.resolveCodexTicketAccountProxy(ctx, account)
		proxy.policy.strategy = policy.strategy
		return proxy.policy, err
	}
	if policy.mode == CodexTicketProxyModeRandom {
		return policy, nil
	}
	if policy.mode == CodexTicketProxyModeFixed {
		loader, ok := s.accountRepo.(codexTicketProxyLoader)
		if !ok {
			return policy, errors.New("codex ticket proxy loader is unavailable")
		}
		proxy, err := loader.GetCodexTicketProxy(ctx, policy.proxyID)
		if err != nil || !codexTicketProxyAvailable(proxy) || proxy.ID != policy.proxyID {
			return policy, ErrRandomProxyUnavailable
		}
		resolved := cloneOpenAICodexTicketAccount(account)
		resolved.ProxyID, resolved.Proxy = &proxy.ID, proxy
		resolved.fixedProxyOrigin = nil
		if err := resolveFixedProxyTransportFallback(ctx, resolved, s.accountRepo); err != nil {
			return policy, err
		}
		proxy, policy.proxyID = resolved.Proxy, *resolved.ProxyID
		if err := validateProxyRegion(ctx, proxy, policy.countryCode, s.accountRepo); err != nil {
			return policy, err
		}
		policy.url = proxy.URL()
		return policy, nil
	}
	// 显式全局 random 使用全局均衡池；inherit 和旧版 pool 在回退随机池时
	// 保留 inherited 标记，以复用随机账号自己的池范围。
	policy.mode = OpenAICodexTicketHarvestProxyModeRandom
	if s.settingService != nil {
		var proxyID int64
		policy.mode, policy.url, proxyID, err = s.settingService.GetOpenAICodexTicketHarvestProxySettingsWithID(ctx)
		policy.proxyID = proxyID
		if err != nil {
			return policy, err
		}
	} else {
		policy.url = strings.TrimSpace(s.openAICodexTicketConfigContext(ctx).HarvestProxyURL)
		if policy.url != "" {
			policy.mode = CodexTicketProxyModeFixed
		} else {
			// 没有设置服务时仍按旧版“继承/代理池”行为处理随机账号。
			policy.inherited = true
		}
	}
	if policy.mode == CodexTicketProxyModeInherit {
		policy.url = strings.TrimSpace(s.openAICodexTicketConfigContext(ctx).HarvestProxyURL)
		if policy.url != "" {
			policy.mode = CodexTicketProxyModeFixed
		} else {
			policy.mode = CodexTicketProxyModeRandom
			policy.inherited = true
		}
	} else if policy.mode == OpenAICodexTicketHarvestProxyModePool {
		policy.mode = CodexTicketProxyModeRandom
		policy.inherited = true
	} else if policy.mode == CodexTicketProxyModeAccount {
		policy.inherited, policy.followBusiness = true, true
	}
	if policy.mode == CodexTicketProxyModeFixed && policy.proxyID > 0 {
		loader, ok := s.accountRepo.(codexTicketProxyLoader)
		if !ok {
			return policy, errors.New("codex ticket proxy loader is unavailable")
		}
		proxy, loadErr := loader.GetCodexTicketProxy(ctx, policy.proxyID)
		if loadErr != nil || !codexTicketProxyAvailable(proxy) || proxy.ID != policy.proxyID {
			return policy, ErrRandomProxyUnavailable
		}
		if err := validateProxyRegion(ctx, proxy, policy.countryCode, s.accountRepo); err != nil {
			return policy, err
		}
		policy.url = proxy.URL()
	}
	if policy.mode == "fixed" {
		if policy.proxyID == 0 && policy.countryCode != "" {
			return policy, errors.New("proxy region cannot be verified for the global harvest proxy URL; select a managed proxy")
		}
		return policy, nil
	}
	if policy.mode != CodexTicketProxyModeRandom && policy.mode != CodexTicketProxyModeAccount {
		return policy, errors.New("invalid codex ticket proxy mode")
	}
	return policy, nil
}

func (s *OpenAIGatewayService) reportOpenAICodexTicketProxyResult(ctx context.Context, proxy openAICodexTicketProxy, successful bool) {
	if codexTicketScheduleFrom(ctx) != nil {
		return
	}
	if proxy.accountID <= 0 || proxy.proxyID <= 0 || ctx.Err() != nil {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 500*time.Millisecond)
	defer cancel()
	var err error
	if successful {
		if reporter, ok := s.accountRepo.(randomProxySuccessReporter); ok {
			err = reporter.ReportRandomProxySuccess(writeCtx, proxy.accountID, proxy.proxyID)
		}
	} else if reporter, ok := s.accountRepo.(randomProxyFailureReporter); ok {
		err = reporter.ReportRandomProxyFailure(writeCtx, proxy.accountID, proxy.proxyID)
	}
	if err != nil {
		slog.Warn("codex_ticket.report_proxy_result_failed", "account_id", proxy.accountID, "proxy_id", proxy.proxyID)
	}
}

// 打票出口独立于账号的业务出口，故障必须上报本次实际选中的池代理。
func (s *OpenAIGatewayService) reportOpenAICodexTicketProxyFailure(ctx context.Context, proxy openAICodexTicketProxy, cause error) {
	if codexTicketScheduleFrom(ctx) != nil {
		return
	}
	var transportError *codexTicketTransportError
	if proxy.accountID <= 0 || proxy.proxyID <= 0 || ctx.Err() != nil || !errors.As(cause, &transportError) || !isRandomProxyTransportFailure(transportError) {
		return
	}
	reporter, ok := s.accountRepo.(randomProxyFailureReporter)
	if !ok {
		return
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 500*time.Millisecond)
	defer cancel()
	if err := reporter.ReportRandomProxyFailure(writeCtx, proxy.accountID, proxy.proxyID); err != nil {
		slog.Warn("codex_ticket.report_proxy_failure_failed", "account_id", proxy.accountID, "proxy_id", proxy.proxyID)
	}
}
