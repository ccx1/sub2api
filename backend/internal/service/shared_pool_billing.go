package service

import (
	"context"
	"encoding/json"
	"math"
	"strconv"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/shopspring/decimal"
)

var (
	ErrSharedPoolEarningsEmpty  = infraerrors.BadRequest("SHARED_POOL_EARNINGS_EMPTY", "暂无可转入余额的共享收益")
	ErrSharedPoolBillingInvalid = infraerrors.BadRequest("SHARED_POOL_BILLING_INVALID", "共享账号归属、分组或分成配置无效")
)

type SharedPoolEarningsFilter struct {
	Page      int
	PageSize  int
	AccountID int64
	Status    string
}

type SharedPoolEarning struct {
	ID                   int64     `json:"id"`
	AccountID            int64     `json:"account_id"`
	OwnerUserID          int64     `json:"owner_user_id"`
	GroupID              int64     `json:"group_id"`
	BillingAmount        float64   `json:"billing_amount"`
	PlatformRateBPS      int       `json:"platform_rate_bps"`
	ProxyRateBPS         int       `json:"proxy_rate_bps"`
	UsesPlatformProxy    bool      `json:"uses_platform_proxy"`
	PlatformAmount       float64   `json:"platform_amount"`
	OwnerAmount          float64   `json:"owner_amount"`
	Status               string    `json:"status"`
	TransferID           *int64    `json:"transfer_id,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
	BaseAmount           *float64  `json:"base_amount,omitempty"`
	SettlementMultiplier *float64  `json:"settlement_multiplier,omitempty"`
	SettlementAmount     *float64  `json:"settlement_amount,omitempty"`
	SpreadAmount         *float64  `json:"spread_amount,omitempty"`
}

type SharedPoolEarningsPage struct {
	Items    []SharedPoolEarning `json:"items"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

type SharedPoolEarningsSummary struct {
	TotalEarned    float64 `json:"total_earned"`
	Available      float64 `json:"available"`
	Pending        float64 `json:"pending"`
	Transferred    float64 `json:"transferred"`
	PlatformAmount float64 `json:"platform_amount"`
	BillingAmount  float64 `json:"billing_amount"`
}

// SharedPoolUserAccountTier describes the subscription tier distribution of a
// contributor's shared accounts.
type SharedPoolUserAccountTier struct {
	Tier  string `json:"tier"`
	Count int64  `json:"count"`
}

// SharedPoolUserEarnings 按供号用户聚合共享收益，供管理员查看每个用户的实际收益。
type SharedPoolUserEarnings struct {
	UserID         int64                       `json:"user_id"`
	Email          string                      `json:"email"`
	AccountCount   int64                       `json:"account_count"`
	AccountTiers   []SharedPoolUserAccountTier `json:"account_tiers"`
	EarningsCount  int64                       `json:"earnings_count"`
	BillingAmount  float64                     `json:"billing_amount"`
	TotalEarned    float64                     `json:"total_earned"`
	PlatformAmount float64                     `json:"platform_amount"`
	Available      float64                     `json:"available"`
	Pending        float64                     `json:"pending"`
	Transferred    float64                     `json:"transferred"`
}

type SharedPoolEarningsTransfer struct {
	ID      int64   `json:"id"`
	Amount  float64 `json:"amount"`
	Balance float64 `json:"balance"`
}

type SharedPoolAccountEarnings struct {
	TodayEarnings float64 `json:"today_earnings"`
	TotalEarnings float64 `json:"total_earnings"`
}

type SharedPoolAccountWindowEarnings struct {
	BillingAmount float64 `json:"billing_amount"`
	OwnerAmount   float64 `json:"owner_amount"`
}

type SharedPoolEarningsRepository interface {
	List(context.Context, int64, SharedPoolEarningsFilter) (*SharedPoolEarningsPage, error)
	Summary(context.Context, int64) (*SharedPoolEarningsSummary, error)
	UserEarnings(context.Context) ([]SharedPoolUserEarnings, error)
	Transfer(context.Context, int64) (*SharedPoolEarningsTransfer, error)
	AccountTotals(context.Context, int64, []int64) (map[int64]SharedPoolAccountEarnings, error)
	AccountWindow(context.Context, int64, int64, time.Time) (*SharedPoolAccountWindowEarnings, error)
}

// SplitSharedPoolBillingAmount 只舍入平台金额，供应者取差额，保证八位金额守恒。
func SplitSharedPoolBillingAmount(amount float64, platformBPS, proxyBPS int) (decimal.Decimal, decimal.Decimal, error) {
	if amount < 0 || math.IsNaN(amount) || math.IsInf(amount, 0) || platformBPS < 0 || proxyBPS < 0 || platformBPS > 10000 || proxyBPS > 10000-platformBPS {
		return decimal.Zero, decimal.Zero, ErrSharedPoolBillingInvalid
	}
	base := decimal.NewFromFloat(amount).Round(UsageBillingMonetaryScale)
	platform := base.Mul(decimal.NewFromInt(int64(platformBPS + proxyBPS))).Div(decimal.NewFromInt(10000)).Round(UsageBillingMonetaryScale)
	return platform, base.Sub(platform), nil
}

func applySharedPoolBillingSnapshot(cmd *UsageBillingCommand, account *Account, key *APIKey, log *UsageLog) {
	if cmd == nil || account == nil {
		return
	}
	cmd.SharedPoolOwnerID = sharedPoolBillingOwnerID(account.Extra["shared_pool_owner_id"])
	if cmd.SharedPoolOwnerID <= 0 {
		return
	}
	if key != nil && key.GroupID != nil {
		cmd.GroupID = *key.GroupID
	}
	if log != nil && log.GroupID != nil {
		cmd.GroupID = *log.GroupID
	}
	cmd.SharedPoolGroup = key != nil && key.Group != nil && key.Group.IsSharedPool && key.Group.SubscriptionType == SubscriptionTypeStandard
	if SharedPoolDispatchConsented(account) && key != nil && sharedPoolDispatchGroupType(key.Group) {
		if terms := account.SharedPoolSettlement; terms.Valid() {
			snapshot := *terms
			cmd.SharedPoolSettlementMultiplier = &snapshot.Multiplier
			cmd.SharedPoolPlatformRateBPS = &snapshot.PlatformRateBPS
			cmd.SharedPoolProxyRateBPS = &snapshot.ProxyRateBPS
			if log != nil {
				cmd.SharedPoolBaseCost = log.TotalCost
			}
		}
	}
	cmd.UsesPlatformProxy = account.IsRandomProxy() && account.ProxyID != nil && *account.ProxyID > 0
	if cmd.UsesPlatformProxy {
		id := *account.ProxyID
		cmd.SharedPoolProxyID = &id
	}
}

func sharedPoolBillingOwnerID(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		if v > 0 && v < math.MaxInt64 && v == math.Trunc(v) {
			return int64(v)
		}
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	return 0
}
