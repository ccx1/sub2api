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
	mode      string
	proxyID   int64
	url       string
	inherited bool
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
	if policy.mode == "fixed" {
		proxy := openAICodexTicketProxy{url: policy.url, proxyID: policy.proxyID, policy: policy}
		if policy.proxyID > 0 {
			loaded, err := s.accountRepo.(codexTicketProxyLoader).GetCodexTicketProxy(ctx, policy.proxyID)
			if err != nil || !codexTicketProxyAvailable(loaded) || loaded.URL() != policy.url {
				return openAICodexTicketProxy{}, ErrRandomProxyUnavailable
			}
			proxy.proxyName = loaded.Name
		}
		return proxy, nil
	}
	selector, ok := s.accountRepo.(BalancedProxySelector)
	if !ok {
		return openAICodexTicketProxy{}, errors.New("codex ticket proxy pool is not configured")
	}
	selection := ProxyPoolSelection{AccountID: account.ID}
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
	return openAICodexTicketProxy{url: proxy.URL(), accountID: account.ID, proxyID: proxy.ID, proxyName: proxy.Name, policy: policy}, nil
}

func (s *OpenAIGatewayService) codexTicketProxyPolicy(ctx context.Context, account *Account) (codexTicketProxyPolicy, error) {
	if account == nil || ValidateCodexTicketProxyExtra(account.Extra) != nil {
		return codexTicketProxyPolicy{}, errors.New("invalid codex ticket proxy settings")
	}
	policy := codexTicketProxyPolicy{mode: account.CodexTicketProxyMode(), proxyID: account.CodexTicketProxyID()}
	if policy.mode == CodexTicketProxyModeAccount {
		proxy, err := s.resolveCodexTicketAccountProxy(ctx, account)
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
		policy.url = proxy.URL()
		return policy, nil
	}
	policy.mode, policy.inherited = "pool", true
	if strings.TrimSpace(s.openAICodexTicketConfig().HarvestProxyURL) != "" {
		policy.mode = "fixed"
	}
	if s.settingService != nil {
		var err error
		policy.mode, policy.url, err = s.settingService.GetOpenAICodexTicketHarvestProxySettings(ctx)
		return policy, err
	}
	if policy.mode == "fixed" {
		policy.url = strings.TrimSpace(s.openAICodexTicketConfigContext(ctx).HarvestProxyURL)
		return policy, nil
	}
	if policy.mode != "pool" {
		return policy, errors.New("invalid codex ticket proxy mode")
	}
	return policy, nil
}

func (s *OpenAIGatewayService) reportOpenAICodexTicketProxyResult(ctx context.Context, proxy openAICodexTicketProxy, successful bool) {
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
