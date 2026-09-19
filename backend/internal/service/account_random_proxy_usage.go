package service

import (
	"context"
	"log/slog"
	"time"
)

const RandomProxyLastUsedExtraKey = "random_proxy_last_used"

// 只保留管理端可展示的出口信息，不保存代理用户名、密码或完整 URL。
type RandomProxyUsage struct {
	ProxyID   *int64 `json:"proxy_id"`
	ProxyName string `json:"proxy_name"`
	ProxyHost string `json:"proxy_host"`
	ProxyPort int    `json:"proxy_port"`
	UsedAt    string `json:"used_at"`
}

type randomProxyUsageRecorder interface {
	RecordRandomProxyUsage(context.Context, int64, RandomProxyUsage) error
}

func RecordRandomProxyUsage(ctx context.Context, account *Account, source any) {
	if account == nil || !account.IsRandomProxy() {
		return
	}
	recorder, ok := source.(randomProxyUsageRecorder)
	if !ok {
		return
	}
	usage := RandomProxyUsage{UsedAt: time.Now().UTC().Format("2006-01-02T15:04:05.000000000Z")}
	if account.ProxyID != nil && account.Proxy != nil {
		id := account.Proxy.ID
		usage.ProxyID, usage.ProxyName = &id, account.Proxy.Name
		usage.ProxyHost, usage.ProxyPort = account.Proxy.Host, account.Proxy.Port
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 250*time.Millisecond)
	defer cancel()
	if err := recorder.RecordRandomProxyUsage(writeCtx, account.ID, usage); err != nil {
		slog.Warn("random_proxy.record_usage_failed", "account_id", account.ID, "error", err)
	}
}
