package service

import (
	"encoding/json"
	"fmt"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// SharedPoolSubscriptionGroupIDs maps a platform and subscription tier to the
// ordered list of groups that should receive matching accounts. The custom
// decoder keeps settings written by older clients (a single numeric ID) valid.
type SharedPoolSubscriptionGroupIDs map[string]map[string][]int64

func (groups *SharedPoolSubscriptionGroupIDs) UnmarshalJSON(data []byte) error {
	var raw map[string]map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		*groups = nil
		return nil
	}
	parsed := make(SharedPoolSubscriptionGroupIDs, len(raw))
	for platform, tiers := range raw {
		parsed[platform] = make(map[string][]int64, len(tiers))
		for tier, value := range tiers {
			var ids []int64
			if err := json.Unmarshal(value, &ids); err != nil {
				var id int64
				if err := json.Unmarshal(value, &id); err != nil {
					return fmt.Errorf("subscription_group_ids.%s.%s must be an ID or an array of IDs: %w", platform, tier, err)
				}
				ids = []int64{id}
			}
			if ids == nil {
				ids = []int64{}
			}
			parsed[platform][tier] = ids
		}
	}
	*groups = parsed
	return nil
}

// NormalizeSharedSubscriptionGroupIDs removes duplicate IDs while preserving
// their configured order and rejects unsupported platforms or invalid IDs.
func NormalizeSharedSubscriptionGroupIDs(groups SharedPoolSubscriptionGroupIDs) (SharedPoolSubscriptionGroupIDs, error) {
	normalized := make(SharedPoolSubscriptionGroupIDs, len(groups))
	for platform, tiers := range groups {
		if !sharedPlatformSupported(platform) {
			return nil, infraerrors.BadRequest("INVALID_SHARED_SUBSCRIPTION_GROUP", "订阅档位分组平台不受支持")
		}
		normalized[platform] = make(map[string][]int64, len(tiers))
		for tier, ids := range tiers {
			if tier == "" {
				return nil, infraerrors.BadRequest("INVALID_SHARED_SUBSCRIPTION_GROUP", "订阅档位不能为空")
			}
			seen := make(map[int64]bool, len(ids))
			for _, id := range ids {
				if id <= 0 {
					return nil, infraerrors.BadRequest("INVALID_SHARED_SUBSCRIPTION_GROUP", "订阅档位分组 ID 必须为正整数")
				}
				if !seen[id] {
					normalized[platform][tier] = append(normalized[platform][tier], id)
					seen[id] = true
				}
			}
			if len(normalized[platform][tier]) > 50 {
				return nil, infraerrors.BadRequest("TOO_MANY_GROUPS", "每个订阅档位最多选择50个分组")
			}
		}
	}
	return normalized, nil
}
