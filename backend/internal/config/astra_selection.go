package config

import "fmt"

func (c CodexGatewayPinConfig) UsesGroups() bool {
	return c.SourceSelection == "groups" || c.TargetSelection == "groups"
}

func (c CodexGatewayPinConfig) ValidateSelections() error {
	for _, selection := range []struct {
		mode string
		ids  []int64
	}{{c.SourceSelection, c.SourceGroupIDs}, {c.TargetSelection, c.TargetGroupIDs}} {
		if selection.mode != "" && selection.mode != "accounts" && selection.mode != "groups" {
			return fmt.Errorf("astra_selection_invalid")
		}
		if len(selection.ids) > 100 || (c.Enabled && selection.mode == "groups" && len(selection.ids) == 0) {
			return fmt.Errorf("astra_group_selection_invalid")
		}
		seen := map[int64]bool{}
		for _, id := range selection.ids {
			if id <= 0 || seen[id] {
				return fmt.Errorf("astra_group_selection_invalid")
			}
			seen[id] = true
		}
	}
	return nil
}

// 分组成员只在读取时解析，不能把客户端预览或旧成员快照保存为授权范围。
func AstraStoredSettings(value AstraRoutingSettings) AstraRoutingSettings {
	value.SelectionError = ""
	if value.CookiePool.SourceSelection == "groups" {
		value.CookiePool.SourceAccountIDs = nil
	}
	if value.CookiePool.TargetSelection == "groups" {
		value.CookiePool.TargetAccountIDs = nil
	}
	return value
}

func ValidateAstraResolvedAccounts(value AstraRoutingSettings) error {
	pool := value.CookiePool
	if !pool.Enabled && !value.WSSession.Enabled {
		return nil
	}
	if len(pool.SourceAccountIDs) == 0 {
		return fmt.Errorf("astra_group_accounts_empty")
	}
	if len(pool.TargetAccountIDs) == 0 {
		return fmt.Errorf("astra_target_required")
	}
	if len(pool.SourceAccountIDs) > 64 || len(pool.TargetAccountIDs) > 64 {
		return fmt.Errorf("astra_group_accounts_limit")
	}
	sources, targets := map[int64]bool{}, map[int64]bool{}
	for _, id := range pool.SourceAccountIDs {
		sources[id] = true
	}
	for _, id := range pool.TargetAccountIDs {
		if sources[id] {
			return fmt.Errorf("astra_account_overlap")
		}
		targets[id] = true
	}
	if value.WSSession.Enabled && pool.TargetSelection == "groups" {
		for _, id := range value.WSSession.AccountIDs {
			if !sources[id] && !targets[id] {
				return fmt.Errorf("astra_ws_outside_targets")
			}
		}
	}
	return nil
}
