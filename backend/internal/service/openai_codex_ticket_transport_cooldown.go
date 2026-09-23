package service

import "context"

// 采集与业务复验可能使用不同出口，只能上报与本次请求 URL 一致的代理快照。
func (s *OpenAIGatewayService) reportCodexProbeConnectionFailure(ctx context.Context, in openAICodexTicketProbeInput, cause error) {
	if in.Account == nil || in.ProxyURL == "" || !isProxyConnectionFailure(cause) {
		return
	}
	var proxy *Proxy
	if !codexTicketProbeBusiness(in) || in.QualityVerification {
		if schedule := codexTicketScheduleFrom(ctx); schedule != nil && schedule.reservation != nil {
			proxy = schedule.reservation.Proxy
		}
	}
	if proxy == nil && in.Account.Proxy != nil && in.Account.Proxy.URL() == in.ProxyURL {
		proxy = in.Account.Proxy
	}
	if proxy == nil && in.HarvestProxyPolicy != nil && in.HarvestProxyPolicy.proxyID > 0 {
		if loader, ok := s.accountRepo.(codexTicketProxyLoader); ok {
			proxy, _ = loader.GetCodexTicketProxy(ctx, in.HarvestProxyPolicy.proxyID)
		}
	}
	if proxy != nil && proxy.URL() == in.ProxyURL {
		reportProxyConnectionFailure(ctx, in.Account.ID, proxy, s.accountRepo, cause)
	}
}
