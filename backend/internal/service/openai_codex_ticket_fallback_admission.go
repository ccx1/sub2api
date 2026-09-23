package service

import "context"

// 准入只解析固定代理的实际备用出口；随机代理仍由后续调度按票据绑定选择。
func (s *OpenAIGatewayService) codexTicketAdmissionAccount(ctx context.Context, account *Account) *Account {
	if account == nil || account.IsRandomProxy() || account.ProxyID == nil || *account.ProxyID <= 0 {
		return account
	}
	if _, ok := s.accountRepo.(fixedProxyFailoverResolver); !ok {
		return account
	}
	resolved := cloneOpenAICodexTicketAccount(account)
	if err := ResolveRandomProxyFromSource(ctx, resolved, s.accountRepo); err != nil {
		return nil
	}
	return resolved
}
