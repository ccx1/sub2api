package repository

import (
	"context"
	"errors"
	"slices"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/lib/pq"
)

func (r *settingRepository) ResolveAstraRoutingAccounts(ctx context.Context, value config.AstraRoutingSettings) (config.AstraRoutingSettings, error) {
	return resolveAstraRoutingAccounts(ctx, r.client, value)
}

func resolveAstraRoutingAccounts(ctx context.Context, exec sqlExecutor, value config.AstraRoutingSettings) (config.AstraRoutingSettings, error) {
	value = config.AstraStoredSettings(value)
	if !value.CookiePool.Enabled && !value.WSSession.Enabled {
		return value, nil
	}
	selection := value.CookiePool
	selection.Enabled = true
	if err := selection.ValidateSelections(); err != nil {
		return value, err
	}
	for _, side := range []struct {
		mode     string
		groups   []int64
		accounts *[]int64
	}{
		{selection.SourceSelection, selection.SourceGroupIDs, &value.CookiePool.SourceAccountIDs},
		{selection.TargetSelection, selection.TargetGroupIDs, &value.CookiePool.TargetAccountIDs},
	} {
		if side.mode != "groups" {
			continue
		}
		ids, err := astraGroupAccountIDs(ctx, exec, side.groups)
		if err != nil {
			return config.AstraStoredSettings(value), err
		}
		*side.accounts = ids
	}
	resolved, err := config.ResolveAstraDependencies(value)
	if err != nil {
		return config.AstraStoredSettings(value), err
	}
	if err = config.ValidateAstraResolvedAccounts(resolved); err != nil {
		return config.AstraStoredSettings(value), err
	}
	return resolved, nil
}

func astraGroupAccountIDs(ctx context.Context, exec sqlExecutor, groupIDs []int64) ([]int64, error) {
	if err := astraValidateLiveSelectionGroups(ctx, exec, groupIDs); err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx, `/* astra_group_accounts */
 SELECT DISTINCT a.id FROM accounts a
 JOIN account_groups ag ON ag.account_id=a.id
 JOIN groups g ON g.id=ag.group_id
 WHERE ag.group_id=ANY($1) AND g.deleted_at IS NULL AND g.status='active' AND g.platform='openai'
 AND a.deleted_at IS NULL AND a.status='active' AND a.platform='openai' AND a.type='oauth'
 AND a.parent_account_id IS NULL AND (a.expires_at IS NULL OR a.expires_at>NOW())
 ORDER BY a.id`, pq.Array(groupIDs))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.Sort(ids)
	return slices.Compact(ids), nil
}

func astraValidateLiveSelectionGroups(ctx context.Context, exec sqlExecutor, ids []int64) error {
	rows, err := exec.QueryContext(ctx, `/* astra_selection_groups */
 SELECT id FROM groups WHERE id=ANY($1)
 AND deleted_at IS NULL AND status='active' AND platform='openai' ORDER BY id`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	found := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		found[id] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if !found[id] {
			return errors.New("astra_group_unavailable")
		}
	}
	return nil
}

func astraSelectionGroupIDs(value config.AstraRoutingSettings) []int64 {
	ids := []int64{}
	if value.CookiePool.SourceSelection == "groups" {
		ids = append(ids, value.CookiePool.SourceGroupIDs...)
	}
	if value.CookiePool.TargetSelection == "groups" {
		ids = append(ids, value.CookiePool.TargetGroupIDs...)
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// 与 BindGroups 的账号行锁互斥；必须拿锁后再解析成员，避免等待锁期间成员已移出。
func lockAstraSelectionAccounts(ctx context.Context, exec sqlExecutor, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	rows, err := exec.QueryContext(ctx, `/* astra_selection_account_lock */
 SELECT id FROM accounts WHERE id=ANY($1) AND deleted_at IS NULL ORDER BY id FOR UPDATE`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	found := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		found[id] = true
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if !found[id] {
			return errors.New("astra_account_unavailable")
		}
	}
	return nil
}

func astraRoutePreparationUnchanged(ctx context.Context, exec sqlExecutor, previous, value config.AstraRoutingSettings) (bool, error) {
	if config.AstraRouteSettingsEqual(config.AstraStoredSettings(previous), config.AstraStoredSettings(value)) {
		return true, nil
	}
	if previous.Revision != value.Revision || !previous.CookiePool.UsesGroups() || !value.CookiePool.UsesGroups() {
		return false, nil
	}
	// 动态来源可能扣除了旧策略中的手选目标；仅调度保存仍应保留账号原有映射和 WS 配置。
	resolved, err := resolveAstraRoutingAccounts(ctx, exec, previous)
	if err != nil {
		return false, err
	}
	return config.AstraRouteSettingsEqual(resolved, value), nil
}
