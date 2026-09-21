package service

import "context"

// 只存在于请求内的结算快照，不进入账号存储或调度缓存。
type SharedPoolSettlementTerms struct {
	Multiplier              float64
	PlatformRateBPS         int
	ProxyRateBPS            int
	ConsumerTokenMultiplier float64
	ConsumerImageMultiplier float64
}

type SharedPoolSettlementSource interface {
	SharedPoolSettlementTerms(context.Context, int64) (*SharedPoolSettlementTerms, error)
}

// SharedPoolAccountSettlementSource is an optional extension implemented by
// repositories that can resolve the administrator's platform/tier multiplier.
// The legacy source remains supported for older test doubles and deployments.
type SharedPoolAccountSettlementSource interface {
	SharedPoolSettlementTermsForAccount(context.Context, int64, string, string) (*SharedPoolSettlementTerms, error)
}

type SharedPoolDispatchGroupSource interface {
	SharedPoolDispatchGroup(context.Context, int64) (*Group, error)
}

func (t *SharedPoolSettlementTerms) Valid() bool {
	return t != nil && ValidSharedPoolSettlementMultiplier(t.Multiplier) &&
		t.PlatformRateBPS >= 0 && t.ProxyRateBPS >= 0 && t.PlatformRateBPS <= 10000-t.ProxyRateBPS
}
