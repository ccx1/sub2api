package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	SharedPoolOwnerKey                = "shared_pool_owner_id"
	SharedPoolEnabledKey              = "shared_pool_enabled"
	SharedPoolAdminDisabledKey        = "shared_pool_admin_disabled"
	SharedPoolDispatchConsentKey      = "shared_pool_dispatch_consent"
	SharedPoolSettlementMultiplierKey = "shared_pool_settlement_multiplier"
	SharedPoolSubscriptionTierKey     = "shared_pool_subscription_tier"
)

var ErrSharedPoolAccountNotFound = infraerrors.NotFound("SHARED_ACCOUNT_NOT_FOUND", "共享账号不存在")

type sharedPoolSearchContextKey struct{}

func WithSharedPoolSearch(ctx context.Context, search string) context.Context {
	return context.WithValue(ctx, sharedPoolSearchContextKey{}, search)
}
func SharedPoolSearch(ctx context.Context) string {
	value, _ := ctx.Value(sharedPoolSearchContextKey{}).(string)
	return value
}

type SharedPoolSettings struct {
	DefaultPriority      int                            `json:"default_priority"`
	PlatformRateBPS      int                            `json:"platform_rate_bps"`
	ProxyRateBPS         int                            `json:"proxy_rate_bps"`
	MaxConcurrency       int                            `json:"max_concurrency"`
	DefaultGroupIDs      SharedPoolDefaultGroupIDs      `json:"default_group_ids"`
	SubscriptionGroupIDs SharedPoolSubscriptionGroupIDs `json:"subscription_group_ids"`
	// SubscriptionSettlementMultipliers stores the platform/tier settlement
	// multiplier configured by an administrator. A user-specific override in
	// shared_pool_user_rates still takes precedence over this map.
	SubscriptionSettlementMultipliers map[string]map[string]float64 `json:"subscription_settlement_multipliers"`
	SettlementMultiplier              float64                       `json:"settlement_multiplier"`
}

type SharedPoolUserRate struct {
	UserID               int64    `json:"user_id"`
	Email                string   `json:"email"`
	PlatformRateBPS      *int     `json:"platform_rate_bps"`
	ProxyRateBPS         *int     `json:"proxy_rate_bps"`
	SettlementMultiplier *float64 `json:"settlement_multiplier"`
}

type SharedPoolAccountRecord struct {
	Assigned      bool
	AccountID     int64
	OwnerUserID   int64
	OwnerEmail    string
	Enabled       bool
	AdminDisabled bool
}

type SharedPoolAccountInput struct {
	Name               string                   `json:"name"`
	Platform           string                   `json:"platform"`
	Type               string                   `json:"type"`
	Concurrency        int                      `json:"concurrency"`
	ProxyURL           *string                  `json:"proxy_url"`
	ProtectionEnabled  bool                     `json:"protection_enabled"`
	CodexTicketEnabled *bool                    `json:"codex_ticket_enabled"`
	ExcelBPSEnabled    *bool                    `json:"excel_bps_enabled"`
	ExcelBPSOptions    *ExcelBPSOptions         `json:"excel_bps_options,omitempty"`
	ConfirmDisable     bool                     `json:"confirm_disable"`
	Enabled            bool                     `json:"enabled"`
	DispatchConsent    bool                     `json:"dispatch_consent"`
	Credentials        map[string]any           `json:"credentials"`
	DailyCooldown      *SharedPoolDailyCooldown `json:"daily_cooldown,omitempty"`
}

type SharedPoolAccountUpdate struct {
	Name          string
	Concurrency   int
	ProxyChanged  bool
	ProxyID       *int64
	Credentials   map[string]any
	Fingerprint   string
	DailyCooldown *SharedPoolDailyCooldown
	// ExcelBPSChanged 时先清除整族 BPS 键，再写入 ExcelBPSExtra；nil 表示关闭协议。
	ExcelBPSChanged bool
	ExcelBPSExtra   map[string]any
}

type SharedPoolGroupView struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type SharedPoolAccountState struct {
	OwnerID          int64
	Enabled          *bool
	AdminDisabled    *bool
	GroupIDs         *[]int64
	DispatchConsent  *bool
	SubscriptionTier *string
	Priority         *int
	// DefaultGroupIDs 仅用于首次开启时自动分配，事务内不得覆盖已有分配。
	DefaultGroupIDs []int64
}

type SharedPoolAccountView struct {
	ID                       int64    `json:"id"`
	OwnerUserID              int64    `json:"owner_user_id,omitempty"`
	OwnerEmail               string   `json:"owner_email,omitempty"`
	Name                     string   `json:"name"`
	Platform                 string   `json:"platform"`
	SubscriptionTier         string   `json:"subscription_tier,omitempty"`
	SubscriptionTierOverride string   `json:"subscription_tier_override,omitempty"`
	Type                     string   `json:"type"`
	Concurrency              int      `json:"concurrency"`
	Priority                 int      `json:"priority"`
	Enabled                  bool     `json:"enabled"`
	AdminDisabled            bool     `json:"admin_disabled"`
	DispatchConsent          bool     `json:"dispatch_consent"`
	SettlementMultiplier     *float64 `json:"settlement_multiplier"`
	Status                   string   `json:"status"`
	ErrorMessage             string   `json:"error_message"`
	ProxyMode                string   `json:"proxy_mode"`
	HasCustomProxy           bool     `json:"has_custom_proxy"`
	ProtectionEnabled        bool     `json:"protection_enabled"`
	CodexTicketEnabled       *bool    `json:"codex_ticket_enabled,omitempty"`
	// 保留旧客户端响应字段；共享账号不再按订阅档位强制打票。
	CodexTicketRequired bool                     `json:"codex_ticket_required"`
	ExcelBPSEnabled     *bool                    `json:"excel_bps_enabled,omitempty"`
	ExcelBPSOptions     *ExcelBPSOptions         `json:"excel_bps_options,omitempty"`
	DailyCooldown       *SharedPoolDailyCooldown `json:"daily_cooldown,omitempty"`
	GroupIDs            []int64                  `json:"group_ids"`
	Groups              []SharedPoolGroupView    `json:"groups"`
	TodayEarnings       float64                  `json:"today_earnings"`
	TotalEarnings       float64                  `json:"total_earnings"`
	EstimatedEarnings   *float64                 `json:"estimated_earnings"`
	PlatformRateBPS     int                      `json:"platform_rate_bps"`
	ProxyRateBPS        int                      `json:"proxy_rate_bps"`
	LastUsedAt          *time.Time               `json:"last_used_at"`
	CreatedAt           time.Time                `json:"created_at"`
}

type SharedPoolAccountPage struct {
	Items    []SharedPoolAccountView `json:"items"`
	Total    int64                   `json:"total"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"page_size"`
}

type SharedPoolRepository interface {
	SharedSettings(context.Context) (*SharedPoolSettings, error)
	SaveSharedSettings(context.Context, *SharedPoolSettings) error
	SharedUserRates(context.Context) ([]SharedPoolUserRate, error)
	SaveSharedUserRate(context.Context, SharedPoolUserRate) error
	GetSharedAccount(context.Context, int64, int64) (*SharedPoolAccountRecord, error)
	ListSharedAccounts(context.Context, int64, int, int) ([]SharedPoolAccountRecord, int64, error)
	CreateSharedAccount(context.Context, *Account, int64, string) error
	UpdateSharedAccount(context.Context, int64, int64, SharedPoolAccountUpdate) error
	SetSharedAccountState(context.Context, int64, SharedPoolAccountState) error
	RemoveSharedAccount(context.Context, int64, int64) error
	CreateSharedProxy(context.Context, int64, *Proxy, string) (*Proxy, error)
}

// SharedPoolSharingAllowed 独立于管理员的 schedulable 字段，防止刷新凭证重新开启用户暂停的共享。
func SharedPoolSharingAllowed(a *Account) bool {
	if a == nil {
		return false
	}
	if _, shared := a.Extra[SharedPoolOwnerKey]; !shared {
		return true
	}
	enabled, _ := a.Extra[SharedPoolEnabledKey].(bool)
	disabled, _ := a.Extra[SharedPoolAdminDisabledKey].(bool)
	return enabled && !disabled
}

func PreserveSharedPoolExtra(current, incoming map[string]any) {
	for _, key := range []string{SharedPoolOwnerKey, SharedPoolEnabledKey, SharedPoolAdminDisabledKey, SharedPoolDispatchConsentKey, SharedPoolSettlementMultiplierKey, SharedPoolSubscriptionTierKey} {
		if value, ok := current[key]; ok {
			incoming[key] = value
		} else {
			delete(incoming, key)
		}
	}
}
