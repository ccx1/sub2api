package config

import (
	"fmt"
	"slices"
)

// ResolveAstraDependencies returns a detached configuration. WS targets reuse
// qualified HTTP routes; sources remain donors rather than becoming consumers.
func ResolveAstraDependencies(value AstraRoutingSettings) (AstraRoutingSettings, error) {
	if value.CookiePool.RotateNodes {
		value.CookiePool.IPAffinity = true
	}
	value.SchedulingGroupIDs = append([]int64(nil), value.SchedulingGroupIDs...)
	value.CookiePool.SourceGroupIDs = append([]int64(nil), value.CookiePool.SourceGroupIDs...)
	value.CookiePool.TargetGroupIDs = append([]int64(nil), value.CookiePool.TargetGroupIDs...)
	value.CookiePool.SourceAccountIDs = append([]int64(nil), value.CookiePool.SourceAccountIDs...)
	value.CookiePool.TargetAccountIDs = append([]int64(nil), value.CookiePool.TargetAccountIDs...)
	value.WSSession.AccountIDs = append([]int64(nil), value.WSSession.AccountIDs...)
	sources := map[int64]bool{}
	for _, id := range value.CookiePool.SourceAccountIDs {
		sources[id] = true
	}
	// 来源优先；手选目标和实时分组成员使用同一排除规则，WS 不能将来源加回目标。
	value.CookiePool.TargetAccountIDs = slices.DeleteFunc(value.CookiePool.TargetAccountIDs, func(id int64) bool { return sources[id] })
	if value.WSSession.Enabled {
		value.CookiePool.Enabled = true
		targets := map[int64]bool{}
		for _, id := range value.CookiePool.TargetAccountIDs {
			targets[id] = true
		}
		for _, id := range value.WSSession.AccountIDs {
			canExpand := value.CookiePool.TargetSelection != "groups" && (value.CookiePool.SourceSelection != "groups" || len(value.CookiePool.SourceAccountIDs) > 0)
			if canExpand && !sources[id] && !targets[id] {
				value.CookiePool.TargetAccountIDs = append(value.CookiePool.TargetAccountIDs, id)
				targets[id] = true
			}
		}
	}
	if value.CookiePool.Enabled && value.CookiePool.SourceSelection != "groups" && len(value.CookiePool.SourceAccountIDs) == 0 {
		return value, fmt.Errorf("astra_source_required")
	}
	if value.CookiePool.Enabled && value.CookiePool.TargetSelection != "groups" && len(value.CookiePool.TargetAccountIDs) == 0 {
		return value, fmt.Errorf("astra_target_required")
	}
	return value, value.Validate()
}
