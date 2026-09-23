package service

import (
	"context"
	"errors"
	"fmt"
)

func validateProxyRegion(ctx context.Context, proxy *Proxy, country string, source any) error {
	if country == "" {
		return nil
	}
	if proxy == nil || proxy.ID <= 0 {
		return errors.New("proxy region cannot be verified for direct egress or an unmanaged proxy")
	}
	matcher, ok := source.(ProxyRegionMatcher)
	if !ok {
		return errors.New("proxy region verification is unavailable")
	}
	matches, err := matcher.MatchesProxyRegion(ctx, proxy, country)
	if err != nil {
		return fmt.Errorf("verify proxy region: %w", err)
	}
	if !matches {
		return fmt.Errorf("proxy exit country is unknown, stale, or does not match required country %s", country)
	}
	return nil
}

func validateFixedAccountProxyRegion(ctx context.Context, account *Account, source any) error {
	country, err := account.ProxyRegionCountry()
	if err != nil || country == "" {
		return err
	}
	if account.ProxyID == nil || *account.ProxyID <= 0 {
		return validateProxyRegion(ctx, nil, country, source)
	}
	loader, ok := source.(codexTicketProxyLoader)
	if !ok {
		return errors.New("proxy region verification requires the current configured proxy")
	}
	// 重新读取代理地址，避免使用新探测结果验证调度快照中的旧出口。
	proxy, err := loader.GetCodexTicketProxy(ctx, *account.ProxyID)
	if err != nil {
		return err
	}
	if !codexTicketProxyAvailable(proxy) || proxy.ID != *account.ProxyID {
		return ErrRandomProxyUnavailable
	}
	if err := validateProxyRegion(ctx, proxy, country, source); err != nil {
		return err
	}
	account.Proxy = proxy
	return nil
}

func validateFixedProxyRegionForReuse(ctx context.Context, current, bound *Account, source any) error {
	country, err := current.ProxyRegionCountry()
	if err != nil || country == "" {
		return err
	}
	if err := validateFixedAccountProxyRegion(ctx, current, source); err != nil {
		return err
	}
	if bound.ProxyID == nil || current.ProxyID == nil || *bound.ProxyID != *current.ProxyID ||
		bound.Proxy == nil || current.Proxy == nil || bound.Proxy.URL() != current.Proxy.URL() {
		return ErrRandomProxyChanged
	}
	return validateProxyRegion(ctx, bound.Proxy, country, source)
}
