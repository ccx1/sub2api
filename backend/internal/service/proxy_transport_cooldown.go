package service

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"strings"
	"time"
)

type proxyTransportFailureReporter interface {
	ReportProxyTransportFailure(context.Context, int64, *Proxy) error
}

type fixedProxyFailoverResolver interface {
	ResolveFixedProxyFailover(context.Context, *Account) (*Proxy, error)
}

// 仅明确的建连故障触发全局冷却；业务状态及流中断继续使用原有账号级策略。
func isProxyConnectionFailure(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return true
	}
	for cause := err; cause != nil; cause = errors.Unwrap(cause) {
		message := strings.ToLower(cause.Error())
		for _, signal := range []string{"tls handshake", "proxyconnect tcp", "proxy connect failed", "proxy connection failed", "connection refused", "no route to host"} {
			if strings.Contains(message, signal) {
				return true
			}
		}
	}
	return false
}

func reportProxyConnectionFailure(ctx context.Context, accountID int64, proxy *Proxy, source any, cause error) bool {
	if ctx.Err() != nil || proxy == nil || proxy.ID <= 0 || !isProxyConnectionFailure(cause) {
		return false
	}
	reporter, ok := source.(proxyTransportFailureReporter)
	if !ok {
		return false
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 500*time.Millisecond)
	defer cancel()
	if err := reporter.ReportProxyTransportFailure(writeCtx, accountID, proxy); err != nil {
		slog.Warn("proxy.transport_cooldown_failed", "account_id", accountID, "proxy_id", proxy.ID)
		return false
	}
	return true
}

func resolveFixedProxyTransportFallback(ctx context.Context, account *Account, source any) error {
	if account.ProxyID == nil || *account.ProxyID <= 0 {
		return nil
	}
	resolver, ok := source.(fixedProxyFailoverResolver)
	if !ok {
		return nil
	}
	configured := account.ConfiguredProxySnapshot()
	if configured.Proxy == nil || configured.Proxy.ID != *configured.ProxyID {
		if loader, available := source.(codexTicketProxyLoader); available {
			proxy, err := loader.GetCodexTicketProxy(ctx, *configured.ProxyID)
			if err != nil {
				return err
			}
			configured = cloneOpenAICodexTicketAccount(configured)
			configured.Proxy = proxy
		}
	}
	proxy, err := resolver.ResolveFixedProxyFailover(ctx, configured)
	if err != nil {
		return err
	}
	if !codexTicketProxyAvailable(proxy) {
		return ErrRandomProxyUnavailable
	}
	if proxy.ID != *configured.ProxyID {
		if configured.Proxy == nil {
			return ErrRandomProxyUnavailable
		}
		origin := *configured.Proxy
		account.fixedProxyOrigin = &origin
	} else {
		account.fixedProxyOrigin = nil
	}
	account.ProxyID, account.Proxy = &proxy.ID, proxy
	return nil
}

// ConfiguredProxySnapshot 供配置比较及持久化使用；票据出口校验仍使用运行时 Proxy。
func (a *Account) ConfiguredProxySnapshot() *Account {
	if a == nil || a.fixedProxyOrigin == nil {
		return a
	}
	copy := *a
	copy.Proxy, copy.ProxyID = a.fixedProxyOrigin, &a.fixedProxyOrigin.ID
	copy.fixedProxyOrigin = nil
	return &copy
}
