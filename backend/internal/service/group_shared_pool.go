package service

import (
	"context"
	"math"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// SharedPoolGroupAvailability 使用持久化归属和运行状态决定共享池能否公开。
type SharedPoolGroupAvailability interface {
	SharedPoolAvailableGroupIDs(ctx context.Context, groupIDs []int64) (map[int64]bool, error)
}

type SharedPoolGroupAccountCounts interface {
	SharedPoolAvailableAccountCounts(ctx context.Context, groupIDs []int64) (map[int64]int64, error)
}

type SharedPoolCapacity struct {
	TotalAccounts                  int64
	AvailableAccounts              int64
	ConcurrencyCapacity            int64
	ConcurrencyUnlimited           bool
	UnlimitedAccounts              int64                             `json:"-"`
	AvailableAccountIDs            []int64                           `json:"-"`
	UntrackedConcurrencyAccountIDs []int64                           `json:"-"`
	TicketAccounts                 []SharedPoolTicketAccountSnapshot `json:"-"`
}

type SharedPoolGroupCapacities interface {
	SharedPoolAvailableCapacities(ctx context.Context, groupIDs []int64) (map[int64]SharedPoolCapacity, error)
}

type SharedPoolGroupBindings interface {
	HasSharedPoolAccounts(ctx context.Context, groupID int64) (bool, error)
	UpdateWithoutSharedPool(ctx context.Context, group *Group) error
}

func (s *adminServiceImpl) validateSharedPoolRemoval(ctx context.Context, group *Group, input *UpdateGroupInput) error {
	if !group.IsSharedPool || input.IsSharedPool == nil || *input.IsSharedPool {
		return nil
	}
	bindings, ok := s.groupRepo.(SharedPoolGroupBindings)
	if !ok {
		return infraerrors.BadRequest("SHARED_POOL_BINDINGS_UNAVAILABLE", "暂时无法核验共享账号关联，请稍后重试")
	}
	bound, err := bindings.HasSharedPoolAccounts(ctx, group.ID)
	if err != nil {
		return err
	}
	if bound {
		return infraerrors.BadRequest("SHARED_POOL_HAS_ACCOUNTS", "请先迁移或移除该分组的共享账号，再关闭共享池标记")
	}
	return nil
}

func ValidateSharedPoolGroup(group *Group) error {
	if group == nil || !group.IsSharedPool {
		return nil
	}
	if group.IsExclusive || group.SubscriptionType != SubscriptionTypeStandard || group.Platform == PlatformComposite {
		return infraerrors.BadRequest("INVALID_SHARED_POOL_GROUP", "共享池仅支持非专属的标准平台分组，不支持订阅或聚合分组")
	}
	return nil
}

// IsSharedPoolAccountAvailable 忽略瞬时并发占满，避免共享池列表随请求抖动。
func IsSharedPoolAccountAvailable(group *Group, account *Account) bool {
	if !sharedPoolDispatchGroupCompatible(group, account) {
		return false
	}
	consented := SharedPoolDispatchConsented(account)
	if consented && !sharedPoolCatalogPriceCoversSettlement(group, account.SharedPoolSettlement) {
		return false
	}
	if group.Platform != account.Platform || !account.IsSchedulable() {
		return false
	}
	if group.RequireOAuthOnly && account.Type != AccountTypeOAuth {
		return false
	}
	return !group.RequirePrivacySet || account.IsPrivacySet()
}

func sharedPoolDispatchGroupType(group *Group) bool {
	return group != nil && (group.SubscriptionType == SubscriptionTypeStandard || group.SubscriptionType == SubscriptionTypeSubscription)
}

// 供号准入与消费权限分开：管理员可关联订阅/专属分组，消费方仍须通过原鉴权。
func sharedPoolDispatchGroupCompatible(group *Group, account *Account) bool {
	if account == nil || !sharedPoolDispatchGroupType(group) || !group.IsActive() ||
		group.Platform != account.Platform || group.Platform == PlatformComposite {
		return false
	}
	if SharedPoolDispatchConsented(account) {
		return true
	}
	if _, modern := account.Extra[SharedPoolDispatchConsentKey]; modern {
		return false
	}
	return group.IsSharedPool && !group.IsExclusive && group.SubscriptionType == SubscriptionTypeStandard
}

func sharedPoolCatalogPriceCoversSettlement(group *Group, terms *SharedPoolSettlementTerms) bool {
	if !terms.Valid() || math.IsNaN(group.RateMultiplier) || math.IsInf(group.RateMultiplier, 0) || group.RateMultiplier <= 0 {
		return false
	}
	token, image := computePeakAwareMultipliers(&APIKey{Group: group}, group.RateMultiplier, time.Now())
	effective := token
	// 目录没有请求类型，独立图片价格按较低值保守判断；实际用户折扣由请求准入复核。
	if group.ImageRateIndependent {
		effective = math.Min(token, image)
	}
	return !math.IsNaN(effective) && !math.IsInf(effective, 0) && effective > 0 && effective >= terms.Multiplier
}

// 目录只扩展调用者本来有权使用的分组，关联供号账号不会授予消费分组权限。
func (s *APIKeyService) SharedAccountCatalogGroups(ctx context.Context, groups []Group) ([]Group, error) {
	provider, ok := s.groupRepo.(SharedPoolGroupCapacities)
	if !ok {
		return nil, nil
	}
	ids := make([]int64, 0, len(groups))
	for _, g := range groups {
		if sharedPoolDispatchGroupType(&g) && g.IsActive() && g.Platform != PlatformComposite {
			ids = append(ids, g.ID)
		}
	}
	capacities, err := provider.SharedPoolAvailableCapacities(ctx, ids)
	if err != nil {
		return nil, err
	}
	result := make([]Group, 0)
	for _, g := range groups {
		capacity, found := capacities[g.ID]
		if !found || capacity.AvailableAccounts == 0 {
			continue
		}
		g.SharedPoolCapacity, g.ActiveAccountCount = &capacity, capacity.AvailableAccounts
		result = append(result, g)
	}
	return result, nil
}

func (s *APIKeyService) availableSharedPoolGroups(ctx context.Context, groups []Group) (map[int64]bool, error) {
	return availableSharedPoolGroupIDs(ctx, s.groupRepo, groups)
}

func availableSharedPoolGroupIDs(ctx context.Context, repo GroupRepository, groups []Group) (map[int64]bool, error) {
	ids := make([]int64, 0)
	for i := range groups {
		if groups[i].IsSharedPool && groups[i].IsActive() && ValidateSharedPoolGroup(&groups[i]) == nil {
			ids = append(ids, groups[i].ID)
		}
	}
	if len(ids) == 0 {
		return map[int64]bool{}, nil
	}
	if provider, ok := repo.(SharedPoolGroupCapacities); ok {
		capacities, err := provider.SharedPoolAvailableCapacities(ctx, ids)
		if err != nil {
			return nil, err
		}
		available := make(map[int64]bool, len(capacities))
		for i := range groups {
			if groups[i].IsSharedPool {
				capacity := capacities[groups[i].ID]
				groups[i].SharedPoolCapacity = &capacity
				groups[i].ActiveAccountCount = capacity.AvailableAccounts
				available[groups[i].ID] = capacity.AvailableAccounts > 0
			}
		}
		return available, nil
	}
	if counter, ok := repo.(SharedPoolGroupAccountCounts); ok {
		counts, err := counter.SharedPoolAvailableAccountCounts(ctx, ids)
		if err != nil {
			return nil, err
		}
		available := make(map[int64]bool, len(counts))
		for i := range groups {
			if groups[i].IsSharedPool {
				groups[i].ActiveAccountCount = counts[groups[i].ID]
				available[groups[i].ID] = counts[groups[i].ID] > 0
			}
		}
		return available, nil
	}
	availability, ok := repo.(SharedPoolGroupAvailability)
	if !ok {
		// 未实现能力时关闭公开入口，不退化为仅凭 active 标记放行。
		return map[int64]bool{}, nil
	}
	return availability.SharedPoolAvailableGroupIDs(ctx, ids)
}
