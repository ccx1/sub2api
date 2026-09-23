package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"strings"
	"time"
)

type randomProxyFailureReporter interface {
	ReportRandomProxyFailure(context.Context, int64, int64) error
}

type randomProxySuccessReporter interface {
	ReportRandomProxySuccess(context.Context, int64, int64) error
}

func ReportRandomProxySuccess(ctx context.Context, account *Account, source any) bool {
	if account == nil || !account.IsRandomProxy() || account.ID <= 0 || account.ProxyID == nil || *account.ProxyID <= 0 {
		return false
	}
	reporter, ok := source.(randomProxySuccessReporter)
	if !ok {
		return false
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 500*time.Millisecond)
	defer cancel()
	if err := reporter.ReportRandomProxySuccess(writeCtx, account.ID, *account.ProxyID); err != nil {
		slog.Warn("random_proxy.report_success_failed", "account_id", account.ID, "proxy_id", *account.ProxyID)
		return false
	}
	return true
}

// 只累计当前账号出口的连续故障；业务状态码和客户端取消不代表代理失效。
func ReportRandomProxyTransportFailure(ctx context.Context, account *Account, source any, cause error) bool {
	if account != nil && account.ProxyID != nil && account.Proxy != nil && account.Proxy.ID == *account.ProxyID &&
		reportProxyConnectionFailure(ctx, account.ID, account.Proxy, source, cause) {
		return true
	}
	if account == nil || !account.IsRandomProxy() || account.ID <= 0 || account.ProxyID == nil || *account.ProxyID <= 0 {
		return false
	}
	if ctx.Err() != nil || !isRandomProxyTransportFailure(cause) {
		return false
	}
	reporter, ok := source.(randomProxyFailureReporter)
	if !ok {
		return false
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 500*time.Millisecond)
	defer cancel()
	if err := reporter.ReportRandomProxyFailure(writeCtx, account.ID, *account.ProxyID); err != nil {
		slog.Warn("random_proxy.report_failure_failed", "account_id", account.ID, "proxy_id", *account.ProxyID)
		return false
	}
	return true
}

func isRandomProxyTransportFailure(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var networkError net.Error
	if errors.As(err, &networkError) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || classifyUpstreamTransportError(err).Persistent {
		return true
	}
	message := strings.ToLower(err.Error())
	for _, signal := range []string{"connection reset", "broken pipe", "tls handshake timeout", "proxyconnect tcp"} {
		if strings.Contains(message, signal) {
			return true
		}
	}
	return false
}
