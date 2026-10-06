package service

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

type astraGroupResolver interface {
	ResolveAstraRoutingAccounts(context.Context, config.AstraRoutingSettings) (config.AstraRoutingSettings, error)
}

func (s *SettingService) resolveAstraRoutingGroups(ctx context.Context, value config.AstraRoutingSettings) (config.AstraRoutingSettings, error) {
	if !value.CookiePool.UsesGroups() {
		value.SelectionError = ""
		return value, nil
	}
	value = config.AstraStoredSettings(value)
	if !value.CookiePool.Enabled && !value.WSSession.Enabled {
		return value, nil
	}
	resolver, ok := s.settingRepo.(astraGroupResolver)
	if !ok {
		return value, errors.New("astra_group_resolution_unavailable")
	}
	resolved, err := resolver.ResolveAstraRoutingAccounts(ctx, value)
	if err != nil {
		return value, err
	}
	if err := config.ValidateAstraResolvedAccounts(resolved); err != nil {
		return value, err
	}
	return resolved, nil
}

// 失效策略保留分组选择供管理员修正，但不能继续借用旧成员的路由。
func unavailableAstraGroupSelection(value config.AstraRoutingSettings, err error) config.AstraRoutingSettings {
	value = config.AstraStoredSettings(value)
	value.SelectionError = "astra_group_resolution_unavailable"
	switch err.Error() {
	case "astra_group_unavailable", "astra_group_accounts_empty", "astra_group_accounts_limit", "astra_account_overlap", "astra_source_required", "astra_target_required", "astra_ws_outside_targets", "astra_selection_scheduling_conflict":
		value.SelectionError = err.Error()
	}
	// 任一端失效都不继续把另一端认定为有效的借票集合。
	value.CookiePool.SourceAccountIDs = nil
	value.CookiePool.TargetAccountIDs = nil
	return value
}
