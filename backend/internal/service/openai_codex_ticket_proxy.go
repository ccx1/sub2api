package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
)

type openAICodexTicketProxy struct {
	url       string
	accountID int64
	proxyID   int64
}

type codexTicketTransportError struct{ error }

func (e *codexTicketTransportError) Unwrap() error { return e.error }

func (s *OpenAIGatewayService) selectOpenAICodexTicketProxy(ctx context.Context, account *Account) (openAICodexTicketProxy, error) {
	if account == nil || account.IsInDailyCooldown(time.Now()) {
		return openAICodexTicketProxy{}, errors.New("account is unavailable during daily cooldown")
	}
	mode := "pool"
	if strings.TrimSpace(s.openAICodexTicketConfig().HarvestProxyURL) != "" {
		mode = "fixed"
	}
	if s.settingService != nil {
		var err error
		mode, err = s.settingService.GetOpenAICodexTicketHarvestProxyMode(ctx)
		if err != nil {
			return openAICodexTicketProxy{}, err
		}
	}
	if mode == "fixed" {
		return openAICodexTicketProxy{url: s.openAICodexTicketHarvestProxyURLContext(ctx)}, nil
	}
	if mode != "pool" {
		return openAICodexTicketProxy{}, errors.New("invalid codex ticket proxy mode")
	}
	selector, ok := s.accountRepo.(BalancedProxySelector)
	if !ok {
		return openAICodexTicketProxy{}, errors.New("codex ticket proxy pool is not configured")
	}
	selection := ProxyPoolSelection{AccountID: account.ID}
	if account.IsRandomProxy() {
		selection.Restricted = account.RandomProxyPoolScope() == RandomProxyPoolSelected
		selection.IDs = account.RandomProxyPoolIDs()
		selection.MaxReuseDuration = account.RandomProxyMaxReuseDuration()
	}
	proxy, err := selector.SelectBalancedProxy(ctx, selection)
	if err != nil {
		return openAICodexTicketProxy{}, err
	}
	if proxy == nil || !proxy.IsActive() || proxy.IsExpired(time.Now()) {
		return openAICodexTicketProxy{}, ErrRandomProxyUnavailable
	}
	return openAICodexTicketProxy{url: proxy.URL(), accountID: account.ID, proxyID: proxy.ID}, nil
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
