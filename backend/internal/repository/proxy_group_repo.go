package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func (r *proxyRepository) ListProxyGroups(ctx context.Context) ([]service.ProxyGroup, error) {
	rows, err := r.sql.QueryContext(ctx, `SELECT g.id, g.name, g.created_at, g.updated_at,
		COUNT(p.id), COUNT(p.id) FILTER (WHERE p.status='active' AND (p.expires_at IS NULL OR p.expires_at > NOW()))
		FROM proxy_groups g LEFT JOIN proxies p ON p.group_id=g.id AND p.deleted_at IS NULL
		GROUP BY g.id ORDER BY g.name, g.id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	groups := make([]service.ProxyGroup, 0)
	for rows.Next() {
		var group service.ProxyGroup
		if err := rows.Scan(&group.ID, &group.Name, &group.CreatedAt, &group.UpdatedAt, &group.ProxyCount, &group.ActiveProxyCount); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (r *proxyRepository) CreateProxyGroup(ctx context.Context, name string) (*service.ProxyGroup, error) {
	var group service.ProxyGroup
	err := scanSingleRow(ctx, r.sql, `INSERT INTO proxy_groups (name) VALUES ($1) RETURNING id, name, created_at, updated_at`,
		[]any{name}, &group.ID, &group.Name, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrProxyGroupNotFound, service.ErrProxyGroupDuplicate)
	}
	return &group, nil
}

func (r *proxyRepository) UpdateProxyGroup(ctx context.Context, id int64, name string) (*service.ProxyGroup, error) {
	var group service.ProxyGroup
	err := scanSingleRow(ctx, r.sql, `UPDATE proxy_groups SET name=$2, updated_at=NOW() WHERE id=$1 RETURNING id, name, created_at, updated_at`,
		[]any{id, name}, &group.ID, &group.Name, &group.CreatedAt, &group.UpdatedAt)
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrProxyGroupNotFound, service.ErrProxyGroupDuplicate)
	}
	return &group, nil
}

func (r *proxyRepository) ProxyGroupExists(ctx context.Context, id int64) (bool, error) {
	var exists bool
	err := scanSingleRow(ctx, r.sql, `SELECT EXISTS(SELECT 1 FROM proxy_groups WHERE id=$1)`, []any{id}, &exists)
	return exists, err
}

func lockProxyGroup(ctx context.Context, exec sqlExecutor, id int64) error {
	var found int64
	err := scanSingleRow(ctx, exec, `SELECT id FROM proxy_groups WHERE id=$1 FOR UPDATE`, []any{id}, &found)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrProxyGroupNotFound
	}
	return err
}

func (r *proxyRepository) DeleteProxyGroup(ctx context.Context, id int64) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockProxyGroup(ctx, tx, id); err != nil {
		return err
	}
	var inUse bool
	err = scanSingleRow(ctx, tx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE deleted_at IS NULL
		AND lower(btrim(extra->>'proxy_mode'))='random'
		AND lower(btrim(extra->>'random_proxy_pool_scope'))='group'
		AND extra->>'random_proxy_group_id'=$1::text)`, []any{id}, &inUse)
	if err != nil {
		return err
	}
	if inUse {
		return service.ErrProxyGroupInUse
	}
	if _, err := tx.ExecContext(ctx, `UPDATE proxies SET group_id=NULL, updated_at=NOW() WHERE group_id=$1`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM proxy_groups WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *proxyRepository) AssignProxyGroup(ctx context.Context, ids []int64, groupID *int64) (int64, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if groupID != nil {
		if err := lockProxyGroup(ctx, tx, *groupID); err != nil {
			return 0, err
		}
	}
	// 所有选中代理必须仍存在，事务锁保证校验与更新之间不能被删除。
	rows, err := tx.QueryContext(ctx, `SELECT id FROM proxies WHERE id=ANY($1) AND deleted_at IS NULL ORDER BY id FOR UPDATE`, pq.Array(ids))
	if err != nil {
		return 0, err
	}
	count := int64(0)
	for rows.Next() {
		count++
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return 0, err
	}
	if count != int64(len(ids)) {
		return 0, service.ErrProxyNotFound
	}
	if _, err := tx.ExecContext(ctx, `UPDATE proxies SET group_id=$2, updated_at=NOW() WHERE id=ANY($1) AND deleted_at IS NULL`, pq.Array(ids), groupID); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return count, nil
}
