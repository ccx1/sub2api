package repository

import (
	"context"
	"database/sql"

	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	dbaccountgroup "github.com/Wei-Shaw/sub2api/ent/accountgroup"
	dbproxy "github.com/Wei-Shaw/sub2api/ent/proxy"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.OpenAITurnAdmissionReader = (*accountRepository)(nil)

// A read-only repeatable-read transaction keeps account, membership, proxy and
// shadow-parent admission fields in one coherent snapshot.
func (r *accountRepository) GetOpenAITurnAdmission(ctx context.Context, id int64) (*service.Account, *service.Account, error) {
	tx, err := r.client.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = tx.Rollback() }()
	reader := &accountRepository{client: tx.Client()}
	account, err := reader.getOpenAITurnAdmissionAccount(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	var parent *service.Account
	if account.IsShadow() {
		parent, err = reader.getOpenAITurnAdmissionAccount(ctx, *account.ParentAccountID)
		if err != nil {
			return nil, nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	return account, parent, nil
}

// 准入只需分组关联及模型限制，保持同一事务快照但不加载完整分组。
func (r *accountRepository) getOpenAITurnAdmissionAccount(ctx context.Context, id int64) (*service.Account, error) {
	m, err := observerAccountQuery(ctx, r.client.Account.Query()).Where(dbaccount.IDEQ(id)).Only(ctx)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrAccountNotFound, nil)
	}
	account := accountEntityToService(m)
	if account == nil {
		return nil, service.ErrAccountNotFound
	}
	if m.ProxyID != nil {
		proxies, err := r.client.Proxy.Query().Where(dbproxy.IDEQ(*m.ProxyID)).All(ctx)
		if err != nil {
			return nil, err
		}
		if len(proxies) > 0 {
			account.Proxy = proxyEntityToService(proxies[0])
		}
	}
	groups, err := r.client.AccountGroup.Query().Where(dbaccountgroup.AccountIDEQ(id)).Order(dbaccountgroup.ByPriority()).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, group := range groups {
		account.GroupIDs = append(account.GroupIDs, group.GroupID)
		account.AccountGroups = append(account.AccountGroups, service.AccountGroup{
			AccountID: group.AccountID, GroupID: group.GroupID, Priority: group.Priority,
			AllowedModels: service.NormalizeGroupAllowedModels(group.AllowedModels), CreatedAt: group.CreatedAt,
		})
	}
	return account, nil
}
