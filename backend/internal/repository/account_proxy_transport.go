package repository

import (
	"context"
	"errors"
	"time"

	dbproxy "github.com/Wei-Shaw/sub2api/ent/proxy"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *accountRepository) ReportProxyTransportFailure(ctx context.Context, accountID int64, proxy *service.Proxy) error {
	return r.proxyPool.ReportTransportFailure(ctx, accountID, proxy)
}

// 固定配置保留在数据库；仅故障冷却期间借用公共池的运行时关联。
func (r *accountRepository) ResolveFixedProxyFailover(ctx context.Context, account *service.Account) (*service.Proxy, error) {
	if account == nil || account.ProxyID == nil {
		return nil, nil
	}
	if r.proxyPool == nil {
		return account.Proxy, nil
	}
	proxy, err := r.GetCodexTicketProxy(ctx, *account.ProxyID)
	if err != nil {
		return nil, err
	}
	if proxy == nil || !proxy.IsActive() || proxy.IsExpired(time.Now()) {
		return nil, service.ErrRandomProxyUnavailable
	}
	cooling, err := r.proxyPool.TransportCoolingDown(ctx, proxy)
	if err != nil || !cooling {
		return proxy, err
	}
	// 私有共享代理不能借公共池出口，也不能把租户账号带出其隔离边界。
	public, err := r.client.Proxy.Query().Where(dbproxy.IDEQ(proxy.ID), excludePrivateSharedProxies).Exist(ctx)
	if err != nil || !public {
		if err != nil {
			return nil, err
		}
		return nil, errors.New("private account proxy is temporarily cooling down")
	}
	selection, err := service.ResolveAccountProxyPoolSelection(ctx, account, r)
	if err != nil {
		return nil, err
	}
	replacement, err := r.proxyPool.Select(ctx, selection)
	if err != nil {
		return nil, err
	}
	if replacement == nil {
		return nil, service.ErrRandomProxyUnavailable
	}
	return replacement, nil
}
