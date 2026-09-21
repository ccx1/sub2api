package repository

import (
	"context"

	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	dbgroup "github.com/Wei-Shaw/sub2api/ent/group"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func filterSharedAccounts(ctx context.Context, query *dbent.AccountQuery) *dbent.AccountQuery {
	filter := service.SharedAccountFilter(ctx)
	if filter != "shared" && filter != "platform" {
		return query
	}
	return query.Where(func(s *entsql.Selector) {
		predicate := "EXISTS (SELECT 1 FROM shared_pool_accounts spa WHERE spa.account_id = " + s.C("id") + ")"
		if filter == "platform" {
			predicate = "NOT " + predicate
		}
		s.Where(entsql.ExprP(predicate))
	})
}

func excludeSharedAccount(s *entsql.Selector) {
	s.Where(entsql.Not(sqljson.HasKey(s.C(dbaccount.FieldExtra), sqljson.Path(service.SharedPoolOwnerKey))))
}

// 账号行先加锁，再读取分组；与共享开关事务保持同一锁顺序。
func validateSharedAccountGroupBindings(ctx context.Context, client *dbent.Client, accountID int64, groupIDs []int64) error {
	account, err := client.Account.Query().Where(dbaccount.IDEQ(accountID), dbaccount.DeletedAtIsNil()).
		Select(dbaccount.FieldID, dbaccount.FieldExtra, dbaccount.FieldPlatform, dbaccount.FieldType).ForUpdate().Only(ctx)
	if err != nil {
		return translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}
	if _, shared := account.Extra[service.SharedPoolOwnerKey]; !shared {
		return nil
	}
	if _, err := client.ExecContext(ctx, "UPDATE shared_pool_accounts SET assigned=TRUE,updated_at=NOW() WHERE account_id=$1", accountID); err != nil {
		return err
	}
	if len(groupIDs) == 0 {
		return nil
	}
	groups, err := client.Group.Query().Where(dbgroup.IDIn(groupIDs...), dbgroup.DeletedAtIsNil()).ForShare().All(ctx)
	if err != nil {
		return err
	}
	wanted := make(map[int64]struct{}, len(groupIDs))
	for _, id := range groupIDs {
		wanted[id] = struct{}{}
	}
	if len(groups) != len(wanted) {
		return infraerrors.BadRequest("INVALID_SHARED_GROUP", "分组不存在或已删除")
	}
	for _, row := range groups {
		group := groupEntityToService(row)
		if !sharedAccountGroupAllowed(group, account.Platform, account.Type, service.SharedPoolDispatchConsented(&service.Account{Extra: account.Extra})) {
			return infraerrors.BadRequest("INVALID_SHARED_GROUP", "分组与账号不兼容，或账号尚未授权参与普通分组调度")
		}
	}
	return nil
}

func sharedAccountGroupAllowed(group *service.Group, platform, kind string, consented bool) bool {
	if group == nil || !group.IsActive() || group.Platform != platform || group.Platform == service.PlatformComposite ||
		(group.RequireOAuthOnly && kind != service.AccountTypeOAuth) {
		return false
	}
	if !consented {
		return group.IsSharedPool && !group.IsExclusive && group.SubscriptionType == service.SubscriptionTypeStandard
	}
	return group.SubscriptionType == service.SubscriptionTypeStandard || group.SubscriptionType == service.SubscriptionTypeSubscription
}

// 默认分配保持标准、非专属分组；管理员手工分配才开放订阅与专属分组。
func sharedAccountDefaultGroupAllowed(group *service.Group, platform, kind string, consented bool) bool {
	return sharedAccountGroupAllowed(group, platform, kind, consented) &&
		!group.IsExclusive && group.SubscriptionType == service.SubscriptionTypeStandard
}

func validateSharedAccountsForGroup(ctx context.Context, client *dbent.Client, accountIDs []int64, groupID int64) error {
	accounts, err := client.Account.Query().Where(dbaccount.IDIn(accountIDs...), dbaccount.DeletedAtIsNil()).
		Select(dbaccount.FieldID, dbaccount.FieldExtra, dbaccount.FieldPlatform, dbaccount.FieldType).Order(dbaccount.ByID()).ForUpdate().All(ctx)
	if err != nil {
		return err
	}
	var target *service.Group
	for _, account := range accounts {
		if _, shared := account.Extra[service.SharedPoolOwnerKey]; !shared {
			continue
		}
		if target == nil {
			group, err := client.Group.Query().Where(dbgroup.IDEQ(groupID), dbgroup.DeletedAtIsNil()).ForShare().Only(ctx)
			if err != nil {
				return translatePersistenceError(err, service.ErrGroupNotFound, nil)
			}
			target = groupEntityToService(group)
		}
		if !sharedAccountGroupAllowed(target, account.Platform, account.Type, service.SharedPoolDispatchConsented(&service.Account{Extra: account.Extra})) {
			return infraerrors.BadRequest("INVALID_SHARED_GROUP", "分组与账号不兼容，或账号尚未授权参与普通分组调度")
		}
	}
	return nil
}
