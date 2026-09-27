package service

import (
	"context"
	"time"
)

// ProxyPoolSelection 的关联仅用于运行时均衡，不写入账号的固定 proxy_id。
type ProxyPoolSelection struct {
	AccountID            int64
	CountryCode          string
	IDs                  []int64
	Restricted           bool
	AllowCountryFallback bool
	MaxReuseDuration     time.Duration
	// FallbackCountryCode 是 CountryCode 没有可用代理时优先尝试的默认代理地区，不受 AllowCountryFallback 控制。
	FallbackCountryCode string
}

type BalancedProxySelector interface {
	SelectBalancedProxy(context.Context, ProxyPoolSelection) (*Proxy, error)
}
