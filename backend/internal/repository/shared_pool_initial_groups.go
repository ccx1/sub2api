package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbgroup "github.com/Wei-Shaw/sub2api/ent/group"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func validateSharedInitialGroups(ctx context.Context, client *dbent.Client, a *service.Account) error {
	for _, id := range a.GroupIDs {
		row, err := client.Group.Query().Where(dbgroup.IDEQ(id), dbgroup.DeletedAtIsNil()).ForShare().Only(ctx)
		if err != nil {
			if dbent.IsNotFound(err) {
				return infraerrors.BadRequest("INVALID_SHARED_GROUP", "共享分组不存在或已删除")
			}
			return err
		}
		group := groupEntityToService(row)
		allowed := sharedAccountDefaultGroupAllowed(group, a.Platform, a.Type, service.SharedPoolDispatchConsented(a))
		if !allowed {
			allowed, err = sharedAccountTierGroupAllowed(ctx, client, a, group)
			if err != nil {
				return err
			}
		}
		if !allowed {
			return infraerrors.BadRequest("INVALID_SHARED_GROUP", "默认共享分组不可用或与账号不兼容")
		}
	}
	return nil
}

// 仅允许已授权 OAuth 账号进入当前套餐规则明确配置的标准专属组。
// 默认分组和管理员手工分配仍沿用各自的校验规则。
func sharedAccountTierGroupAllowed(ctx context.Context, exec sqlExecutor, a *service.Account, g *service.Group) (bool, error) {
	if a == nil || a.Type != service.AccountTypeOAuth || !service.SharedPoolDispatchConsented(a) ||
		!sharedAccountGroupAllowed(g, a.Platform, a.Type, true) || g.SubscriptionType != service.SubscriptionTypeStandard {
		return false, nil
	}
	tier := service.SharedPoolOverviewTierForAccount(a)
	if tier == "unknown" || tier == "api_key" {
		return false, nil
	}
	rows, err := exec.QueryContext(ctx, `SELECT subscription_group_ids FROM shared_pool_settings WHERE id=1 FOR SHARE`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, rows.Err()
	}
	var raw []byte
	if err = rows.Scan(&raw); err != nil {
		return false, err
	}
	var rules service.SharedPoolSubscriptionGroupIDs
	if err = json.Unmarshal(raw, &rules); err != nil {
		return false, err
	}
	return slices.Contains(rules[a.Platform][tier], g.ID), rows.Err()
}

type sharedGroupAssignment struct {
	ids          []int64
	consented    bool
	manual       bool
	tierOverride *string
}

// 调用方已锁定账号、归属和目标分组；授权升级与管理员档位变更尚未写回，
// 校验时必须叠加本事务的有效值，不能只读取旧 extra。
func sharedAutomaticTierGroupAllowed(ctx context.Context, tx *sql.Tx, accountID, groupID int64, assignment sharedGroupAssignment) (bool, error) {
	if !assignment.consented || assignment.manual {
		return false, nil
	}
	a := &service.Account{}
	var credentials, extra []byte
	err := tx.QueryRowContext(ctx, `SELECT platform,type,credentials,extra FROM accounts WHERE id=$1`, accountID).
		Scan(&a.Platform, &a.Type, &credentials, &extra)
	if err != nil {
		return false, err
	}
	if err = json.Unmarshal(credentials, &a.Credentials); err != nil {
		return false, err
	}
	if len(extra) > 0 {
		if err = json.Unmarshal(extra, &a.Extra); err != nil {
			return false, err
		}
	}
	if a.Extra == nil {
		a.Extra = make(map[string]any)
	}
	a.Extra[service.SharedPoolDispatchConsentKey] = assignment.consented
	if assignment.tierOverride != nil {
		a.Extra[service.SharedPoolSubscriptionTierKey] = *assignment.tierOverride
	}
	g := &service.Group{}
	err = tx.QueryRowContext(ctx, `SELECT id,platform,status,subscription_type,is_exclusive,is_shared_pool,require_oauth_only
		FROM groups WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, groupID).
		Scan(&g.ID, &g.Platform, &g.Status, &g.SubscriptionType, &g.IsExclusive, &g.IsSharedPool, &g.RequireOAuthOnly)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return sharedAccountTierGroupAllowed(ctx, tx, a, g)
}
