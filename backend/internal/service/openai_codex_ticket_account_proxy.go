package service

import (
	"context"
	"errors"
	"fmt"
)

// 跟随账号只读取当前真实出口；过期代理由既有 sweep 完成账号改投后再采集。
func (s *OpenAIGatewayService) resolveCodexTicketAccountProxy(ctx context.Context, account *Account) (openAICodexTicketProxy, error) {
	result := openAICodexTicketProxy{policy: codexTicketProxyPolicy{mode: CodexTicketProxyModeAccount, strategy: account.CodexTicketProxyStrategy()}}
	if account == nil || ValidateCodexTicketProxyExtra(account.Extra) != nil {
		return result, errors.New("invalid codex ticket account proxy settings")
	}
	country, err := account.ProxyRegionCountry()
	if err != nil {
		return result, err
	}
	result.policy.countryCode = country
	resolved := cloneOpenAICodexTicketAccount(account)
	if resolved.IsRandomProxy() {
		if err := ResolveRandomProxyFromSource(ctx, resolved, s.accountRepo); err != nil {
			if disableErr := DisableRandomProxyAccountOnUnavailable(ctx, resolved, s.accountRepo, err); disableErr != nil {
				return result, fmt.Errorf("%w; disable random proxy account: %v", err, disableErr)
			}
			return result, err
		}
		result.accountID = account.ID
	} else if resolved.ProxyID != nil {
		loader, ok := s.accountRepo.(codexTicketProxyLoader)
		if !ok {
			return result, errors.New("codex ticket account proxy loader is unavailable")
		}
		proxy, err := loader.GetCodexTicketProxy(ctx, *resolved.ProxyID)
		if err != nil {
			return result, err
		}
		resolved.Proxy = proxy
	}
	if !resolved.IsRandomProxy() {
		if err := ResolveRandomProxyFromSource(ctx, resolved, s.accountRepo); err != nil {
			return result, err
		}
	}
	if resolved.ProxyID == nil {
		return result, nil
	}
	if !codexTicketProxyAvailable(resolved.Proxy) || resolved.Proxy.ID != *resolved.ProxyID {
		return result, ErrRandomProxyUnavailable
	}
	result.url, result.proxyID, result.proxyName = resolved.Proxy.URL(), resolved.Proxy.ID, resolved.Proxy.Name
	result.policy.url, result.policy.proxyID = result.url, result.proxyID
	return result, nil
}
