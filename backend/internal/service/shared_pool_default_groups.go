package service

import (
	"encoding/json"
	"fmt"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type SharedPoolDefaultGroupIDs map[string][]int64

// 历史设置和旧客户端传单个 ID，新响应统一输出数组。
func (groups *SharedPoolDefaultGroupIDs) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		*groups = nil
		return nil
	}
	parsed := make(SharedPoolDefaultGroupIDs, len(raw))
	for platform, value := range raw {
		var ids []int64
		if err := json.Unmarshal(value, &ids); err != nil {
			var id int64
			if err := json.Unmarshal(value, &id); err != nil {
				return fmt.Errorf("default_group_ids.%s must be an ID or an array of IDs: %w", platform, err)
			}
			ids = []int64{id}
		}
		if ids == nil {
			ids = []int64{}
		}
		parsed[platform] = ids
	}
	*groups = parsed
	return nil
}

func NormalizeSharedDefaultGroupIDs(groups SharedPoolDefaultGroupIDs) (SharedPoolDefaultGroupIDs, error) {
	normalized := make(SharedPoolDefaultGroupIDs, len(groups))
	for platform, ids := range groups {
		if !sharedPlatformSupported(platform) {
			return nil, infraerrors.BadRequest("INVALID_SHARED_DEFAULT", "默认分组平台不受支持")
		}
		seen := make(map[int64]bool, len(ids))
		for _, id := range ids {
			if id <= 0 {
				return nil, infraerrors.BadRequest("INVALID_SHARED_DEFAULT", "默认分组 ID 必须为正整数")
			}
			if !seen[id] {
				normalized[platform] = append(normalized[platform], id)
				seen[id] = true
			}
		}
		if len(normalized[platform]) > 50 {
			return nil, infraerrors.BadRequest("TOO_MANY_GROUPS", "每个平台最多选择50个默认分组")
		}
	}
	return normalized, nil
}
