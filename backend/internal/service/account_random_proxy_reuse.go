package service

import (
	"context"
	"errors"
	"time"
)

var ErrRandomProxyChanged = errors.New("random proxy routing changed; reconnect before sending another request")

// 持续连接不能通过修改账号指针换出口；配置或代理失效时必须停止复用。
func ValidateRandomProxyForReuse(ctx context.Context, account *Account, source any) error {
	if account == nil {
		return nil
	}
	repo, ok := source.(interface {
		GetByID(context.Context, int64) (*Account, error)
		RandomProxySelector
	})
	if !ok {
		return nil
	}
	current, err := repo.GetByID(ctx, account.ID)
	if err != nil {
		return err
	}
	if current == nil || !current.IsActive() || !current.Schedulable || current.IsInDailyCooldown(time.Now()) {
		return ErrRandomProxyChanged
	}
	if !parentHealthyForShadow(current, func(id int64) *Account {
		parent, _ := repo.GetByID(ctx, id)
		return parent
	}) {
		return ErrRandomProxyChanged
	}
	if !account.IsRandomProxy() && !current.IsRandomProxy() {
		return nil
	}
	if account.IsRandomProxy() != current.IsRandomProxy() {
		return ErrRandomProxyChanged
	}
	return validateRandomProxyReconnect(ctx, current, account, source, repo)
}

func validateRandomProxyReconnect(ctx context.Context, current, bound *Account, source any, selector RandomProxySelector) error {
	proxy, err := selectAccountRandomProxy(ctx, current, selector)
	if err != nil {
		return err
	}
	if proxy != nil && proxy.IsActive() && !proxy.IsExpired(time.Now()) {
		if bound.ProxyID != nil && *bound.ProxyID == proxy.ID && bound.Proxy != nil && proxy.URL() == bound.Proxy.URL() {
			return nil
		}
		return ErrRandomProxyChanged
	}
	if current.RandomProxyEmptyPoolPolicy() == RandomProxyEmptyPoolPolicyDirect {
		if bound.ProxyID == nil {
			return nil
		}
		return ErrRandomProxyChanged
	}
	cause := randomProxyUnavailable(current)
	if err := DisableRandomProxyAccountOnUnavailable(ctx, current, source, cause); err != nil {
		return err
	}
	return cause
}
