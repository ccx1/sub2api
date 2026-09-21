package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

// 批量复用账号管理的实时占槽计数，不为每个分组分别访问 Redis。
// 不限并发的请求不占槽，无法给出完整已用数；缺依赖或读取失败同样留空。
func (s *APIKeyService) SharedPoolCurrentConcurrency(ctx context.Context, groups []Group, cfg config.OpenAICodexTicketConfig, now time.Time) map[int64]int64 {
	result := make(map[int64]int64)
	groupIDs, accountIDs := sharedPoolConcurrencyMembers(groups, cfg, now)
	for groupID, ids := range groupIDs {
		if len(ids) == 0 {
			result[groupID] = 0
		}
	}
	if len(accountIDs) == 0 || s == nil || s.concurrencyService == nil || s.concurrencyService.cache == nil {
		return result
	}
	counts, err := s.concurrencyService.GetAccountConcurrencyBatch(ctx, accountIDs)
	if err != nil {
		return result
	}
	for groupID, ids := range groupIDs {
		if current, known := sharedPoolConcurrencySum(ids, counts); known {
			result[groupID] = current
		}
	}
	return result
}

func sharedPoolConcurrencyMembers(groups []Group, cfg config.OpenAICodexTicketConfig, now time.Time) (map[int64][]int64, []int64) {
	groupsByID, seen := make(map[int64][]int64), make(map[int64]bool)
	ids := make([]int64, 0)
	for _, group := range groups {
		if group.SharedPoolCapacity == nil {
			continue
		}
		capacity := *group.SharedPoolCapacity
		if group.Platform == PlatformOpenAI {
			capacity = GetSharedPoolCatalogCapacity(group.SharedPoolCapacity, cfg, now)
		}
		if !sharedPoolConcurrencyTracked(capacity) {
			continue
		}
		groupsByID[group.ID] = capacity.AvailableAccountIDs
		for _, id := range capacity.AvailableAccountIDs {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return groupsByID, ids
}

func sharedPoolConcurrencyTracked(capacity SharedPoolCapacity) bool {
	if capacity.ConcurrencyUnlimited || int64(len(capacity.AvailableAccountIDs)) != capacity.AvailableAccounts {
		return false
	}
	untracked := make(map[int64]bool, len(capacity.UntrackedConcurrencyAccountIDs))
	for _, id := range capacity.UntrackedConcurrencyAccountIDs {
		untracked[id] = true
	}
	for _, id := range capacity.AvailableAccountIDs {
		// 保护策略可能为原始并发为 0 的账号回退有限容量，普通请求仍不占槽。
		if untracked[id] {
			return false
		}
	}
	return true
}

func sharedPoolConcurrencySum(ids []int64, counts map[int64]int) (int64, bool) {
	var total int64
	for _, id := range ids {
		count, exists := counts[id]
		if !exists || count < 0 {
			return 0, false
		}
		total += int64(count)
	}
	return total, true
}
