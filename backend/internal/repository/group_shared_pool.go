package repository

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/ent/group"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.SharedPoolGroupAvailability = (*groupRepository)(nil)
var _ service.SharedPoolGroupBindings = (*groupRepository)(nil)
var _ service.SharedPoolGroupCapacities = (*groupRepository)(nil)

func (r *groupRepository) UpdateWithoutSharedPool(ctx context.Context, groupIn *service.Group) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.Group.Query().Where(group.IDEQ(groupIn.ID), group.DeletedAtIsNil()).ForUpdate().Only(ctx); err != nil {
		return translatePersistenceError(err, service.ErrGroupNotFound, nil)
	}
	txRepo := &groupRepository{client: tx.Client(), sql: tx, proxyPool: r.proxyPool}
	bound, err := txRepo.HasSharedPoolAccounts(ctx, groupIn.ID)
	if err != nil {
		return err
	}
	if bound {
		return infraerrors.BadRequest("SHARED_POOL_HAS_ACCOUNTS", "请先迁移或移除该分组的共享账号，再关闭共享池标记")
	}
	if err := txRepo.Update(ctx, groupIn); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *groupRepository) HasSharedPoolAccounts(ctx context.Context, groupID int64) (bool, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT EXISTS (
		SELECT 1 FROM account_groups ag JOIN shared_pool_accounts spa ON spa.account_id = ag.account_id
		JOIN accounts a ON a.id = ag.account_id
		WHERE ag.group_id = $1 AND a.deleted_at IS NULL
		AND COALESCE(a.extra->>'shared_pool_dispatch_consent','false') <> 'true')`, groupID)
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	var exists bool
	if rows.Next() {
		if err := rows.Scan(&exists); err != nil {
			return false, err
		}
	}
	return exists, rows.Err()
}

func (r *groupRepository) SharedPoolAvailableGroupIDs(ctx context.Context, groupIDs []int64) (map[int64]bool, error) {
	counts, err := r.SharedPoolAvailableAccountCounts(ctx, groupIDs)
	available := make(map[int64]bool)
	for id, count := range counts {
		available[id] = count > 0
	}
	return available, err
}

func (r *groupRepository) SharedPoolAvailableAccountCounts(ctx context.Context, groupIDs []int64) (map[int64]int64, error) {
	capacities, err := r.SharedPoolAvailableCapacities(ctx, groupIDs)
	counts := make(map[int64]int64, len(capacities))
	for id, capacity := range capacities {
		counts[id] = capacity.AvailableAccounts
	}
	return counts, err
}

func (r *groupRepository) SharedPoolAvailableCapacities(ctx context.Context, groupIDs []int64) (map[int64]service.SharedPoolCapacity, error) {
	available := make(map[int64]service.SharedPoolCapacity)
	if len(groupIDs) == 0 {
		return available, nil
	}
	members, err := r.sharedPoolMembers(ctx, groupIDs)
	if err != nil || len(members) == 0 {
		return available, err
	}
	accountIDs := make([]int64, 0, len(members))
	for id := range members {
		accountIDs = append(accountIDs, id)
	}
	accounts, err := r.client.Account.Query().Where(account.IDIn(accountIDs...), account.DeletedAtIsNil()).WithProxy().All(ctx)
	if err != nil {
		return nil, err
	}
	accountSnapshots := make([]*service.Account, 0, len(accounts))
	for _, row := range accounts {
		acc := accountEntityToService(row)
		if row.Edges.Proxy != nil {
			acc.Proxy = proxyEntityToService(row.Edges.Proxy)
		}
		accountSnapshots = append(accountSnapshots, acc)
	}
	settlements, err := r.sharedPoolSettlementByAccounts(ctx, accountSnapshots)
	if err != nil {
		return nil, err
	}
	groups, err := r.client.Group.Query().Where(group.IDIn(groupIDs...), group.DeletedAtIsNil()).All(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*service.Group, len(groups))
	for _, row := range groups {
		byID[row.ID] = groupEntityToService(row)
	}
	proxies, err := r.sharedPoolProxies(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for _, acc := range accountSnapshots {
		acc.SharedPoolSettlement = settlements[acc.ID]
		ticket := service.NewSharedPoolTicketAccountSnapshot(acc, now)
		for _, groupID := range members[acc.ID] {
			if byID[groupID] == nil {
				continue
			}
			capacity := available[groupID]
			capacity.TotalAccounts++
			accountAvailable := proxies.usable(acc) && service.IsSharedPoolAccountAvailable(byID[groupID], acc)
			if ticket != nil && byID[groupID].Platform == service.PlatformOpenAI {
				candidate := *ticket
				candidate.Available, candidate.Concurrency = accountAvailable, acc.Mode1EffectiveConcurrency()
				capacity.TicketAccounts = append(capacity.TicketAccounts, candidate)
			}
			if accountAvailable {
				capacity.AvailableAccounts++
				capacity.AvailableAccountIDs = append(capacity.AvailableAccountIDs, acc.ID)
				if acc.Concurrency <= 0 {
					capacity.UntrackedConcurrencyAccountIDs = append(capacity.UntrackedConcurrencyAccountIDs, acc.ID)
				}
				concurrency := acc.Mode1EffectiveConcurrency()
				if concurrency <= 0 {
					capacity.ConcurrencyUnlimited = true
					capacity.UnlimitedAccounts++
				} else {
					capacity.ConcurrencyCapacity += int64(concurrency)
				}
			}
			available[groupID] = capacity
		}
	}
	return available, nil
}

func (r *groupRepository) sharedPoolMembers(ctx context.Context, groupIDs []int64) (map[int64][]int64, error) {
	rows, err := r.sql.QueryContext(ctx, `
		SELECT ag.account_id, ag.group_id
		FROM account_groups ag
		JOIN shared_pool_accounts spa ON spa.account_id = ag.account_id
		JOIN users owner ON owner.id = spa.owner_user_id
		WHERE ag.group_id = ANY($1) AND spa.enabled AND NOT spa.admin_disabled
		  AND owner.deleted_at IS NULL AND owner.status = $2`, pq.Array(groupIDs), service.StatusActive)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	members := make(map[int64][]int64)
	for rows.Next() {
		var accountID, groupID int64
		if err := rows.Scan(&accountID, &groupID); err != nil {
			return nil, err
		}
		members[accountID] = append(members[accountID], groupID)
	}
	return members, rows.Err()
}
