package service

import (
	"context"
	"time"
)

// ProxyPoolSelection 的关联仅用于运行时均衡，不写入账号的固定 proxy_id。
type ProxyPoolSelection struct {
	AccountID        int64
	IDs              []int64
	Restricted       bool
	MaxReuseDuration time.Duration
}

type BalancedProxySelector interface {
	SelectBalancedProxy(context.Context, ProxyPoolSelection) (*Proxy, error)
}
