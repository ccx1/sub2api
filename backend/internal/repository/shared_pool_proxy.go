package repository

import (
	"context"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// excludePrivateSharedProxies 同时用于均衡池和随机池，沿用平台原有健康与亲和算法。
func excludePrivateSharedProxies(s *entsql.Selector) {
	s.Where(entsql.ExprP("NOT EXISTS (SELECT 1 FROM shared_pool_proxies sp WHERE sp.proxy_id = " + s.C("id") + ")"))
}

func (r *sharedPoolRepository) CreateSharedProxy(ctx context.Context, ownerID int64, p *service.Proxy, fingerprint string) (*service.Proxy, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// 对同一用户串行化去重，避免并发创建同一私有代理。
	if _, err = tx.Client().ExecContext(ctx, `SELECT id FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, ownerID); err != nil {
		return nil, err
	}
	rows, err := tx.Client().QueryContext(ctx, `SELECT proxy_id FROM shared_pool_proxies WHERE owner_user_id=$1 AND fingerprint=$2`, ownerID, fingerprint)
	if err != nil {
		return nil, err
	}
	var existing int64
	if rows.Next() {
		err = rows.Scan(&existing)
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	if existing > 0 {
		item, getErr := tx.Client().Proxy.Get(ctx, existing)
		if getErr != nil {
			return nil, getErr
		}
		return proxyEntityToService(item), nil
	}
	item, err := tx.Client().Proxy.Create().SetName(p.Name).SetProtocol(p.Protocol).SetHost(p.Host).
		SetPort(p.Port).SetUsername(p.Username).SetPassword(p.Password).SetStatus(service.StatusActive).Save(ctx)
	if err != nil {
		return nil, err
	}
	_, err = tx.Client().ExecContext(ctx, `INSERT INTO shared_pool_proxies(proxy_id,owner_user_id,fingerprint) VALUES($1,$2,$3)`, item.ID, ownerID, fingerprint)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return proxyEntityToService(item), nil
}
