package repository

import (
	"context"
	"strconv"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"entgo.io/ent/dialect/sql/sqljson"
	dbaccount "github.com/Wei-Shaw/sub2api/ent/account"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 配置关联按账号去重：同一账号的业务出口和打票出口可以指向同一代理。
const fixedProxyAccountBindingsSQL = `SELECT id, proxy_id FROM accounts
WHERE deleted_at IS NULL AND proxy_id IS NOT NULL
 AND COALESCE(lower(btrim(extra->>'proxy_mode')), '') <> 'random'
UNION
SELECT a.id, p.id FROM accounts a
JOIN proxies p ON p.id::text = a.extra->>'codex_ticket_proxy_id'
WHERE a.deleted_at IS NULL
 AND lower(btrim(a.extra->>'codex_ticket_proxy_mode')) = 'fixed'`

func (r *proxyRepository) fixedProxyAccountBindings(ctx context.Context) (map[int64]map[int64]struct{}, error) {
	rows, err := r.sql.QueryContext(ctx, fixedProxyAccountBindingsSQL)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	bindings := make(map[int64]map[int64]struct{})
	for rows.Next() {
		var accountID, proxyID int64
		if err := rows.Scan(&accountID, &proxyID); err != nil {
			return nil, err
		}
		addProxyAccountBinding(bindings, proxyID, accountID)
	}
	return bindings, rows.Err()
}

func addProxyAccountBinding(bindings map[int64]map[int64]struct{}, proxyID, accountID int64) {
	if proxyID <= 0 || accountID <= 0 {
		return
	}
	if bindings[proxyID] == nil {
		bindings[proxyID] = make(map[int64]struct{})
	}
	bindings[proxyID][accountID] = struct{}{}
}

func fixedAccountProxyIDs(account *service.Account) []int64 {
	ids := make([]int64, 0, 2)
	if account.ProxyID != nil && !account.IsRandomProxy() {
		ids = append(ids, *account.ProxyID)
	}
	if account.CodexTicketProxyMode() == service.CodexTicketProxyModeFixed {
		id := account.CodexTicketProxyID()
		if id > 0 && (len(ids) == 0 || ids[0] != id) {
			ids = append(ids, id)
		}
	}
	return ids
}

func appendFixedAccountBindings(fixed map[int64][]string, account *service.Account, candidates map[int64]bool) {
	for _, id := range fixedAccountProxyIDs(account) {
		if candidates[id] {
			fixed[id] = append(fixed[id], strconv.FormatInt(account.ID, 10))
		}
	}
}

func ticketProxyIDIn(ids []int64) func(*entsql.Selector) {
	return func(s *entsql.Selector) {
		values := make([]any, len(ids))
		for i, id := range ids {
			values[i] = id
			// PostgreSQL 比较提取出的文本，避免脏 JSON 或大 ID 被隐式转成 int32。
			if s.Dialect() == dialect.Postgres {
				values[i] = strconv.FormatInt(id, 10)
			}
		}
		s.Where(sqljson.ValueIn(dbaccount.FieldExtra, values, sqljson.Path(service.CodexTicketProxyIDExtraKey)))
	}
}
