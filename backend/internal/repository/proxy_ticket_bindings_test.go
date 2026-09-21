package repository

import (
	"context"
	"strconv"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestTicketProxyPostgresPredicateDoesNotCastUntrustedJSON(t *testing.T) {
	query := entsql.Dialect(dialect.Postgres).Select("id").From(entsql.Table("accounts"))
	ticketProxyIDIn([]int64{1 << 40})(query)
	sql, args := query.Query()
	require.Contains(t, sql, `"extra"->>'codex_ticket_proxy_id'`)
	require.NotContains(t, sql, "::int")
	require.Equal(t, []any{"1099511627776"}, args)
}

func TestTicketProxyAllocatorCountsBothFixedBindingsOnce(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	a, _ := newProxyPoolAllocatorTest(t, 1)
	a.client, a.loadCandidates = client, a.readCandidates
	ctx := context.Background()
	first := createPoolTestProxy(t, client, "business")
	second := createPoolTestProxy(t, client, "ticket")
	id := mustCreateAPIKeyRepoAccount(t, ctx, client, "dual")
	extra := map[string]any{service.CodexTicketProxyModeExtraKey: "fixed", service.CodexTicketProxyIDExtraKey: second.ID}
	require.NoError(t, client.Account.UpdateOneID(id).SetProxyID(first.ID).SetExtra(extra).Exec(ctx))
	fixed, err := a.readFixedAccounts(ctx, []int64{first.ID, second.ID})
	require.NoError(t, err)
	require.Equal(t, []string{strconv.FormatInt(id, 10)}, fixed[first.ID])
	require.Equal(t, []string{strconv.FormatInt(id, 10)}, fixed[second.ID])
	selected, err := a.Select(ctx, service.ProxyPoolSelection{AccountID: 999})
	require.NoError(t, err)
	require.Nil(t, selected, "both configured proxies are occupied")
	extra[service.CodexTicketProxyIDExtraKey] = first.ID
	require.NoError(t, client.Account.UpdateOneID(id).SetExtra(extra).Exec(ctx))
	fixed, err = a.readFixedAccounts(ctx, []int64{first.ID})
	require.NoError(t, err)
	require.Len(t, fixed[first.ID], 1, "same account business and ticket binding count once")
	selected, err = a.Select(ctx, service.ProxyPoolSelection{AccountID: id, IDs: []int64{first.ID}, Restricted: true})
	require.NoError(t, err)
	require.Equal(t, first.ID, selected.ID, "own fixed reference does not consume another slot")
}

func TestTicketProxyLoaderReturnsStatusAndExcludesDeleted(t *testing.T) {
	_, client := newAPIKeyRepoSQLite(t)
	r := &accountRepository{client: client}
	ctx := context.Background()
	proxy := createPoolTestProxy(t, client, "fixed")
	expires := time.Now().Add(-time.Hour)
	require.NoError(t, client.Proxy.UpdateOneID(proxy.ID).SetStatus(service.StatusDisabled).SetExpiresAt(expires).Exec(ctx))
	loaded, err := r.GetCodexTicketProxy(ctx, proxy.ID)
	require.NoError(t, err)
	require.False(t, loaded.IsActive())
	require.True(t, loaded.IsExpired(time.Now()))
	require.NoError(t, client.Proxy.DeleteOneID(proxy.ID).Exec(ctx))
	_, err = r.GetCodexTicketProxy(ctx, proxy.ID)
	require.ErrorIs(t, err, service.ErrProxyNotFound)
}

func TestTicketProxyAdminRecognizesTicketOnlyRandomAccounts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra map[string]any
		want  bool
	}{
		{"legacy inherit", nil, true},
		{"ticket random", map[string]any{"codex_ticket_proxy_mode": "random"}, true},
		{"ticket fixed", map[string]any{"codex_ticket_proxy_mode": "fixed", "codex_ticket_proxy_id": 1}, false},
		{"ticket follows fixed business", map[string]any{"codex_ticket_proxy_mode": "account"}, false},
		{"ticket disabled", map[string]any{"codex_ticket_enabled": false}, false},
		{"business random ticket fixed", map[string]any{"proxy_mode": "random", "codex_ticket_proxy_mode": "fixed", "codex_ticket_proxy_id": 2}, true},
		{"outside business pool", map[string]any{"proxy_mode": "random", "random_proxy_pool_scope": "selected", "random_proxy_pool_ids": []int64{2}}, false},
		{"explicit ticket random has independent pool", map[string]any{"proxy_mode": "random", "random_proxy_pool_scope": "selected", "random_proxy_pool_ids": []int64{2}, "codex_ticket_proxy_mode": "random"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := &service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Extra: tc.extra}
			require.Equal(t, tc.want, proxyPoolAdminAccountMatches(a, 1, 0))
			a.Status = service.StatusDisabled
			require.False(t, proxyPoolAdminAccountMatches(a, 1, 0))
		})
	}
}

func TestTicketProxyCountsUnionFixedAndDynamicAccounts(t *testing.T) {
	r, mock, allocator := newProxyPoolAdminTest(t)
	ctx := context.Background()
	_, err := allocator.Select(ctx, service.ProxyPoolSelection{AccountID: 7})
	require.NoError(t, err)
	mock.ExpectQuery("SELECT id, protocol, host, port, username, password, group_id FROM proxies").WillReturnRows(proxyPoolAdminProxyRows())
	mock.ExpectQuery("SELECT id, name, platform, type, notes, extra, proxy_id, status").WithArgs("{7}").WillReturnRows(
		proxyPoolAdminAccountRows().AddRow(7, "ticket random", "openai", "oauth", nil, []byte(`{"codex_ticket_proxy_mode":"random"}`), 1, service.StatusActive))
	mock.ExpectQuery("(?s)SELECT id, proxy_id FROM accounts.*UNION.*codex_ticket_proxy_id.*codex_ticket_proxy_mode").WillReturnRows(
		sqlmock.NewRows([]string{"id", "proxy_id"}).AddRow(7, 1).AddRow(8, 1).AddRow(8, 1).AddRow(8, 2))
	counts, err := r.GetAccountCountsForProxies(ctx)
	require.NoError(t, err)
	require.Equal(t, map[int64]int64{1: 2, 2: 1}, counts)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestTicketProxyDeletionCountIncludesFixedTicketReference(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	r := newProxyRepositoryWithSQL(nil, db)
	mock.ExpectQuery("(?s)SELECT id, name, platform, type, notes.*codex_ticket_proxy_mode.*codex_ticket_proxy_id").WithArgs(int64(11)).WillReturnRows(
		sqlmock.NewRows([]string{"id", "name", "platform", "type", "notes"}).AddRow(7, "fixed ticket", "openai", "oauth", nil))
	count, err := r.CountAccountsByProxyID(context.Background(), 11)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	require.NoError(t, mock.ExpectationsWereMet())
}
