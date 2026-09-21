package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.SharedPoolOverviewSource = (*sharedPoolRepository)(nil)
var _ service.SharedPoolOverviewSource = (*groupRepository)(nil)

type sharedOverviewRegistration struct {
	enabled       bool
	adminDisabled bool
	groups        []int64
}

type sharedOverviewState struct {
	registrations map[int64]*sharedOverviewRegistration
	groups        map[int64]*service.Group
	terms         map[int64]*service.SharedPoolSettlementTerms
	proxies       *sharedPoolProxyAvailability
}

func (r *sharedPoolRepository) SharedPoolOverviewAccounts(ctx context.Context) ([]service.SharedPoolOverviewAccount, error) {
	reader := newGroupRepositoryWithSQL(r.client, r.db)
	if r.accounts != nil {
		reader.proxyPool = r.accounts.proxyPool
	}
	return reader.SharedPoolOverviewAccounts(ctx)
}

func (r *groupRepository) SharedPoolOverviewAccounts(ctx context.Context) ([]service.SharedPoolOverviewAccount, error) {
	registrations, groupIDs, err := r.sharedOverviewRegistrations(ctx)
	if err != nil || len(registrations) == 0 {
		return []service.SharedPoolOverviewAccount{}, err
	}
	ids := make([]int64, 0, len(registrations))
	for id := range registrations {
		ids = append(ids, id)
	}
	rows, err := r.client.Account.Query().Where(account.IDIn(ids...), account.DeletedAtIsNil()).WithProxy().All(ctx)
	if err != nil {
		return nil, err
	}
	accounts := make([]*service.Account, 0, len(rows))
	for _, row := range rows {
		acc := accountEntityToService(row)
		if row.Edges.Proxy != nil {
			acc.Proxy = proxyEntityToService(row.Edges.Proxy)
		}
		accounts = append(accounts, acc)
	}
	terms, err := r.sharedPoolSettlementByAccounts(ctx, accounts)
	if err != nil {
		return nil, err
	}
	groups, err := r.sharedOverviewGroups(ctx, groupIDs)
	if err != nil {
		return nil, err
	}
	proxies, err := r.sharedPoolProxies(ctx)
	if err != nil {
		return nil, err
	}
	state := sharedOverviewState{registrations: registrations, groups: groups, terms: terms, proxies: proxies}
	return state.snapshots(accounts), nil
}

func (r *groupRepository) sharedOverviewRegistrations(ctx context.Context) (map[int64]*sharedOverviewRegistration, []int64, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT spa.account_id,spa.enabled,spa.admin_disabled,ag.group_id
		FROM shared_pool_accounts spa JOIN accounts a ON a.id=spa.account_id AND a.deleted_at IS NULL
		JOIN users u ON u.id=spa.owner_user_id AND u.deleted_at IS NULL AND u.status=$1
		LEFT JOIN account_groups ag ON ag.account_id=spa.account_id`, service.StatusActive)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	registrations, groups := make(map[int64]*sharedOverviewRegistration), make(map[int64]bool)
	for rows.Next() {
		var id int64
		var groupID sql.NullInt64
		var enabled, disabled bool
		if err := rows.Scan(&id, &enabled, &disabled, &groupID); err != nil {
			return nil, nil, err
		}
		if registrations[id] == nil {
			registrations[id] = &sharedOverviewRegistration{enabled: enabled, adminDisabled: disabled}
		}
		if groupID.Valid {
			registrations[id].groups = append(registrations[id].groups, groupID.Int64)
			groups[groupID.Int64] = true
		}
	}
	groupIDs := make([]int64, 0, len(groups))
	for id := range groups {
		groupIDs = append(groupIDs, id)
	}
	return registrations, groupIDs, rows.Err()
}

func (r *groupRepository) sharedOverviewGroups(ctx context.Context, ids []int64) (map[int64]*service.Group, error) {
	result := make(map[int64]*service.Group)
	if len(ids) == 0 {
		return result, nil
	}
	rows, err := r.client.Group.Query().Where(group.IDIn(ids...), group.DeletedAtIsNil()).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ID] = groupEntityToService(row)
	}
	return result, nil
}

func (s sharedOverviewState) snapshots(accounts []*service.Account) []service.SharedPoolOverviewAccount {
	result, now := make([]service.SharedPoolOverviewAccount, 0, len(accounts)), time.Now()
	for _, acc := range accounts {
		acc.SharedPoolSettlement = s.terms[acc.ID]
		registration := s.registrations[acc.ID]
		result = append(result, service.SharedPoolOverviewAccount{AccountID: acc.ID, Platform: acc.Platform,
			Tier: service.SharedPoolOverviewTierForAccount(acc), Available: s.available(acc), Concurrency: acc.Mode1EffectiveConcurrency(),
			Participating:        registration != nil && registration.enabled && !registration.adminDisabled,
			UntrackedConcurrency: acc.Concurrency <= 0, Ticket: service.NewSharedPoolTicketAccountSnapshot(acc, now)})
	}
	return result
}

func (s sharedOverviewState) available(account *service.Account) bool {
	registration := s.registrations[account.ID]
	if registration == nil || !registration.enabled || registration.adminDisabled || (s.proxies != nil && !s.proxies.usable(account)) {
		return false
	}
	for _, id := range registration.groups {
		if service.IsSharedPoolAccountAvailable(s.groups[id], account) {
			return true
		}
	}
	return false
}
