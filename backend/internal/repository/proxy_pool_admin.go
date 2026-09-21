package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/redis/go-redis/v9"
)

type proxyPoolAdminBinding struct {
	proxyID int64
	version string
}

type proxyPoolAdminProxy struct {
	version string
	groupID int64
}

func NewProxyRepositoryWithProxyPool(client *dbent.Client, sqlDB *sql.DB, rdb *redis.Client) service.ProxyRepository {
	repo := newProxyRepositoryWithSQL(client, sqlDB)
	repo.proxyPoolRedis = rdb
	return repo
}

func (r *proxyRepository) dynamicProxyAccounts(ctx context.Context, proxyIDs []int64) (map[int64][]service.ProxyAccountSummary, error) {
	result := make(map[int64][]service.ProxyAccountSummary)
	if r.proxyPoolRedis == nil {
		return result, nil
	}
	versions, err := r.proxyPoolAdminVersions(ctx, proxyIDs)
	if err != nil {
		return nil, err
	}
	proxyIDs = make([]int64, 0, len(versions))
	for id := range versions {
		proxyIDs = append(proxyIDs, id)
	}
	bindings, err := readProxyPoolAdminBindings(ctx, r.proxyPoolRedis, proxyIDs)
	if err != nil || len(bindings) == 0 {
		return result, err
	}
	ids := make([]int64, 0, len(bindings))
	for id := range bindings {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	rows, err := r.sql.QueryContext(ctx, `SELECT id, name, platform, type, notes, extra, proxy_id, status
FROM accounts WHERE id = ANY($1) AND deleted_at IS NULL ORDER BY id DESC`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		account, err := scanProxyPoolAdminAccount(rows)
		if err != nil {
			return nil, err
		}
		binding := bindings[account.ID]
		proxy := versions[binding.proxyID]
		if binding.version != proxy.version || !proxyPoolAdminAccountMatches(account, binding.proxyID, proxy.groupID) {
			continue
		}
		result[binding.proxyID] = append(result[binding.proxyID], service.ProxyAccountSummary{
			ID: account.ID, Name: account.Name, Platform: account.Platform, Type: account.Type, Notes: account.Notes,
		})
	}
	return result, rows.Err()
}

func scanProxyPoolAdminAccount(rows *sql.Rows) (*service.Account, error) {
	var account service.Account
	var extra []byte
	var proxyID sql.NullInt64
	if err := rows.Scan(&account.ID, &account.Name, &account.Platform, &account.Type, &account.Notes, &extra, &proxyID, &account.Status); err != nil {
		return nil, err
	}
	if len(extra) > 0 {
		if err := json.Unmarshal(extra, &account.Extra); err != nil {
			return nil, err
		}
	}
	if proxyID.Valid {
		account.ProxyID = &proxyID.Int64
	}
	return &account, nil
}

func proxyPoolAdminAccountMatches(account *service.Account, proxyID, groupID int64) bool {
	if account.Status != service.StatusActive {
		return false
	}
	if service.OpenAICodexTicketAccountEnabled(account) && account.CodexTicketProxyMode() == service.CodexTicketProxyModeRandom {
		return true
	}
	if !account.IsRandomProxy() {
		return service.OpenAICodexTicketAccountEnabled(account) && account.CodexTicketProxyMode() != service.CodexTicketProxyModeFixed && account.CodexTicketProxyMode() != service.CodexTicketProxyModeAccount
	}
	if account.RandomProxyPoolScope() == service.RandomProxyPoolGroup {
		return groupID > 0 && groupID == account.RandomProxyGroupID()
	}
	if account.RandomProxyPoolScope() == service.RandomProxyPoolSelected {
		for _, id := range account.RandomProxyPoolIDs() {
			if id == proxyID {
				return true
			}
		}
		return false
	}
	return true
}

func readProxyPoolAdminBindings(ctx context.Context, rdb *redis.Client, proxyIDs []int64) (map[int64]proxyPoolAdminBinding, error) {
	bindings := make(map[int64]proxyPoolAdminBinding)
	for _, proxyID := range proxyIDs {
		id := strconv.FormatInt(proxyID, 10)
		members, err := rdb.SMembers(ctx, proxyPoolBoundAccountsKey(id)).Result()
		if err != nil {
			return nil, err
		}
		pipeline := rdb.Pipeline()
		commands := make(map[string]*redis.SliceCmd, len(members))
		for _, member := range members {
			commands[member] = pipeline.HMGet(ctx, proxyPoolAffinityKey(member), "proxy_id", "version")
		}
		if _, err := pipeline.Exec(ctx); err != nil {
			return nil, err
		}
		for _, member := range members {
			accountID, err := strconv.ParseInt(member, 10, 64)
			if err != nil || accountID <= 0 {
				continue
			}
			binding, err := commands[member].Result()
			if err != nil {
				return nil, err
			}
			if binding[0] != id {
				continue
			}
			version, _ := binding[1].(string)
			bindings[accountID] = proxyPoolAdminBinding{proxyID: proxyID, version: version}
		}
	}
	return bindings, nil
}

func (r *proxyRepository) dynamicProxyAccountCounts(ctx context.Context) (map[int64]int64, error) {
	counts := make(map[int64]int64)
	accounts, err := r.dynamicProxyAccounts(ctx, nil)
	for id, items := range accounts {
		counts[id] = int64(len(items))
	}
	return counts, err
}

func (r *proxyRepository) proxyPoolAdminVersions(ctx context.Context, ids []int64) (map[int64]proxyPoolAdminProxy, error) {
	query := `SELECT id, protocol, host, port, username, password, group_id FROM proxies
WHERE deleted_at IS NULL AND status = 'active' AND (expires_at IS NULL OR expires_at > $1)`
	args := []any{time.Now()}
	if len(ids) > 0 {
		query += " AND id = ANY($2)"
		args = append(args, pq.Array(ids))
	}
	rows, err := r.sql.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	versions := make(map[int64]proxyPoolAdminProxy)
	for rows.Next() {
		var proxy service.Proxy
		var username, password sql.NullString
		var groupID sql.NullInt64
		if err := rows.Scan(&proxy.ID, &proxy.Protocol, &proxy.Host, &proxy.Port, &username, &password, &groupID); err != nil {
			return nil, err
		}
		proxy.Username, proxy.Password = username.String, password.String
		versions[proxy.ID] = proxyPoolAdminProxy{version: proxyPoolAdminProxyVersion(&proxy), groupID: groupID.Int64}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return versions, rows.Close()
}

func proxyPoolAdminProxyVersion(proxy *service.Proxy) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.TrimSpace(proxy.URL()))))
}
