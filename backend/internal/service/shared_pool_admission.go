package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"math"
	"time"
)

type sharedRateResolver func(context.Context, int64, int64, float64) float64

// 抢到槽位后重读持久化账号，排队、粘性及长连接不能用旧授权继续接收新请求。
func sharedPoolAdmissionLatest(ctx context.Context, selected *Account, repo AccountRepository, resolve sharedRateResolver) (*Account, bool, string) {
	if !isSharedPoolBillingAccount(selected) {
		return selected, false, ""
	}
	if repo == nil {
		return selected, true, "shared_state_unavailable"
	}
	latest, err := repo.GetByID(ctx, selected.ID)
	if err != nil || latest == nil {
		return selected, true, "shared_state_unavailable"
	}
	// 拷贝后只附加本次快照，不能污染仓储或缓存返回的共享对象。
	snapshot := *latest
	latest = &snapshot
	latest.SharedPoolSettlement = nil
	if SharedPoolDispatchConsented(latest) {
		var groupAvailable bool
		ctx, groupAvailable = latestSharedPoolDispatchContext(ctx, repo)
		if !groupAvailable {
			return latest, true, "shared_group_unavailable"
		}
		ownerID := sharedPoolBillingOwnerID(latest.Extra[SharedPoolOwnerKey])
		var terms *SharedPoolSettlementTerms
		var err error
		if source, ok := repo.(SharedPoolAccountSettlementSource); ok {
			tier := SharedPoolOverviewTierForAccount(latest)
			terms, err = source.SharedPoolSettlementTermsForAccount(ctx, ownerID, latest.Platform, tier)
		} else if source, ok := repo.(SharedPoolSettlementSource); ok {
			terms, err = source.SharedPoolSettlementTerms(ctx, ownerID)
		} else {
			return latest, true, "shared_settlement_unavailable"
		}
		if err != nil || !terms.Valid() {
			return latest, true, "shared_settlement_unavailable"
		}
		copy := *terms
		latest.SharedPoolSettlement = &copy
	}
	vetoed, reason := sharedPoolAdmission(ctx, latest, resolve)
	return latest, vetoed, reason
}

func latestSharedPoolDispatchContext(ctx context.Context, repo AccountRepository) (context.Context, bool) {
	group, _ := ctx.Value(ctxkey.Group).(*Group)
	source, ok := repo.(SharedPoolDispatchGroupSource)
	if !ok || group == nil || group.ID <= 0 {
		return ctx, false
	}
	latest, err := source.SharedPoolDispatchGroup(ctx, group.ID)
	if err != nil || latest == nil || latest.ID != group.ID {
		return ctx, false
	}
	copy := *latest
	return context.WithValue(ctx, ctxkey.Group, &copy), true
}

func sharedPoolAdmission(ctx context.Context, account *Account, resolve sharedRateResolver) (bool, string) {
	if !isSharedPoolBillingAccount(account) {
		return false, ""
	}
	group, _ := ctx.Value(ctxkey.Group).(*Group)
	if !account.IsSchedulable() || !sharedPoolDispatchGroupCompatible(group, account) {
		return true, "shared_dispatch_unavailable"
	}
	member := false
	for _, id := range account.GroupIDs {
		if id == group.ID {
			member = true
			break
		}
	}
	if !member || (group.RequireOAuthOnly && account.Type != AccountTypeOAuth) || (group.RequirePrivacySet && !account.IsPrivacySet()) {
		return true, "shared_group_not_authorized"
	}
	if !SharedPoolDispatchConsented(account) {
		return !group.IsSharedPool, "shared_legacy_scope"
	}
	if !account.SharedPoolSettlement.Valid() {
		return true, "shared_invalid_settlement"
	}
	rate := account.SharedPoolSettlement.Multiplier
	userID, _ := ctx.Value(ctxkey.UserID).(int64)
	if userID <= 0 || resolve == nil {
		return true, "shared_pricing_unavailable"
	}
	downstream := resolve(ctx, userID, group.ID, group.RateMultiplier)
	at, ok := openAIPricingAtFromContext(ctx)
	if !ok {
		at, ok = gatewayTokenRequestPricingAtFromContext(ctx)
	}
	if !ok {
		at = time.Now()
	}
	token, image := computePeakAwareMultipliers(&APIKey{Group: group}, downstream, at)
	effective := token
	if intent, _ := ctx.Value(ctxkey.OpenAIImageGenerationIntent).(bool); intent {
		effective = math.Min(token, image)
	}
	if _, media := ctx.Value(openAIProfitControlSuppressCtxKey{}).(struct{}); media {
		effective = math.Min(token, image)
	}
	if math.IsNaN(effective) || math.IsInf(effective, 0) || effective <= 0 || effective < rate {
		return true, "shared_settlement_unfunded"
	}
	account.SharedPoolSettlement.ConsumerTokenMultiplier = token
	account.SharedPoolSettlement.ConsumerImageMultiplier = image
	return false, ""
}
