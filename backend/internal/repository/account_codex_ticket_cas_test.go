package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newCodexTicketCASRepo(t *testing.T) (*accountRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return newAccountRepositoryWithSQL(client, db, nil), mock
}

func codexTicketCASAccount() *service.Account {
	return &service.Account{ID: 41, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Credentials: map[string]any{"access_token": "test"}, Extra: map[string]any{}, Status: service.StatusActive, Schedulable: true}
}

func expectCodexTicketCAS(mock sqlmock.Sqlmock) *sqlmock.ExpectedExec {
	// 断言身份和旧值守护存在，不绑定 SQL 的排版或完整实现文本。
	guards := []string{"UPDATE accounts", "CASE WHEN $2::jsonb = 'null'::jsonb",
		"COALESCE(extra, '{}'::jsonb) - $1", "jsonb_build_object($1::text, $2::jsonb)",
		"id = $3", "platform = $4", "type = $5", "credentials = $6::jsonb",
		"proxy_id IS NOT DISTINCT FROM $7", "COALESCE(extra -> $1, 'null'::jsonb) = $8::jsonb",
		"NOT EXISTS", "jsonb_each($9::jsonb)", "COALESCE(extra -> expected.key, 'null'::jsonb) <> expected.value",
		"$2::jsonb = 'null'::jsonb OR $2::jsonb @> '{\"revoked\":true}'::jsonb OR (status = 'active' AND schedulable = true", "NOT auto_pause_on_expired OR expires_at IS NULL OR expires_at > NOW()",
		"deleted_at IS NULL"}
	pattern := "(?s)"
	for _, guard := range guards {
		pattern += regexp.QuoteMeta(guard) + ".*"
	}
	return mock.ExpectExec(pattern)
}

type codexTicketCASJSON string

func (expected codexTicketCASJSON) Match(value driver.Value) bool {
	var got, want any
	raw, ok := value.(string)
	if !ok || json.Unmarshal([]byte(raw), &got) != nil || json.Unmarshal([]byte(expected), &want) != nil {
		return false
	}
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	return string(gotJSON) == string(wantJSON)
}

const codexTicketCASDefaultConfig = codexTicketCASJSON(`{"codex_ticket_enabled":null,"codex_ticket_credential_policy":null,"codex_ticket_proxy_mode":null,"codex_ticket_proxy_id":null,"codex_ticket_proxy_strategy":null,"proxy_mode":null,"random_proxy_empty_pool_policy":null,"random_proxy_pool_scope":null,"random_proxy_pool_ids":null,"random_proxy_group_id":null,"random_proxy_max_reuse_minutes":null,"daily_cooldown":null,"enable_tls_fingerprint":null,"tls_fingerprint_builtin":null,"tls_fingerprint_profile_id":null,"codex_fingerprint_mode":null,"anti_degrade":null,"anti_degradation":null,"shared_pool_owner_id":null,"shared_pool_enabled":null,"shared_pool_admin_disabled":null}`)

func TestCompareAndSwapCodexTicketCreateReplaceDeleteAndConflict(t *testing.T) {
	for _, tc := range []struct {
		name             string
		old, replacement any
		wantOld, wantNew string
		affected         int64
	}{
		{"create", nil, map[string]any{"state": "new"}, "null", `{"state":"new"}`, 1},
		{"replace", map[string]any{"state": "old"}, map[string]any{"state": "new"}, `{"state":"old"}`, `{"state":"new"}`, 1},
		{"delete", map[string]any{"state": "old"}, nil, `{"state":"old"}`, "null", 1},
		{"changed concurrently", map[string]any{"state": "old"}, nil, `{"state":"old"}`, "null", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newCodexTicketCASRepo(t)
			account := codexTicketCASAccount()
			if tc.old != nil {
				account.Extra["codex_turn_ticket:model"] = tc.old
			}
			expectCodexTicketCAS(mock).WithArgs("codex_turn_ticket:model", tc.wantNew, int64(41),
				service.PlatformOpenAI, service.AccountTypeOAuth, `{"access_token":"test"}`, nil, tc.wantOld,
				codexTicketCASDefaultConfig).WillReturnResult(sqlmock.NewResult(0, tc.affected))
			changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, " model ", tc.replacement)
			require.NoError(t, err)
			require.Equal(t, tc.affected == 1, changed)
			require.Equal(t, tc.old, account.Extra["codex_turn_ticket:model"], "repository must not mutate caller snapshots")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCompareAndSwapCodexTicketPreservesRawConfigurationAndRandomProxyIdentity(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	account := codexTicketCASAccount()
	account.Extra = map[string]any{service.OpenAICodexTicketEnabledExtraKey: false,
		service.CodexTicketProxyModeExtraKey: service.CodexTicketProxyModeFixed, service.CodexTicketProxyIDExtraKey: 27,
		service.ProxyModeExtraKey: service.ProxyModeRandom, service.RandomProxyEmptyPoolPolicyExtraKey: "reject",
		service.RandomProxyPoolScopeExtraKey: "selected", service.RandomProxyPoolIDsExtraKey: []int64{9, 10},
		service.RandomProxyMaxReuseMinutesExtraKey: 15, "unrelated": "kept", service.RandomProxyLastUsedExtraKey: "observed",
		"enable_tls_fingerprint": true, "tls_fingerprint_builtin": "nodejs24", "tls_fingerprint_profile_id": 7,
		"codex_fingerprint_mode": "device", service.AntiDegradeMarkerExtraKey: map[string]any{"enabled": true, "mode": "mode1"}, service.AntiDegradationExtraKey: true}
	setCodexTicketCASProxy(account)
	mock.ExpectBegin()
	expectCodexTicketCASProxy(mock, "proxy.example")
	expectCodexTicketCAS(mock).WithArgs("codex_turn_ticket:model", `{"state":"new"}`, int64(41),
		service.PlatformOpenAI, service.AccountTypeOAuth, `{"access_token":"test"}`, nil, "null",
		codexTicketCASJSON(`{"codex_ticket_enabled":false,"codex_ticket_credential_policy":null,"codex_ticket_proxy_mode":"fixed","codex_ticket_proxy_id":27,"codex_ticket_proxy_strategy":null,"proxy_mode":"random","random_proxy_empty_pool_policy":"reject","random_proxy_pool_scope":"selected","random_proxy_pool_ids":[9,10],"random_proxy_group_id":null,"random_proxy_max_reuse_minutes":15,"daily_cooldown":null,"enable_tls_fingerprint":true,"tls_fingerprint_builtin":"nodejs24","tls_fingerprint_profile_id":7,"codex_fingerprint_mode":"device","anti_degrade":{"enabled":true,"mode":"mode1"},"anti_degradation":true,"shared_pool_owner_id":null,"shared_pool_enabled":null,"shared_pool_admin_disabled":null}`)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", map[string]any{"state": "new"})
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, int64(9), *account.ProxyID, "runtime association must remain caller-owned")
	require.Equal(t, "kept", account.Extra["unrelated"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func setCodexTicketCASProxy(account *service.Account) {
	id := int64(9)
	account.ProxyID = &id
	account.Proxy = &service.Proxy{ID: id, Protocol: "http", Host: "proxy.example", Port: 8080, Status: service.StatusActive}
}

func expectCodexTicketCASProxy(mock sqlmock.Sqlmock, host string) {
	mock.ExpectQuery(`(?s)SELECT protocol, host, port.*FROM proxies.*FOR SHARE`).WithArgs(int64(9)).
		WillReturnRows(sqlmock.NewRows([]string{"protocol", "host", "port", "username", "password", "status"}).
			AddRow("http", host, 8080, "", "", service.StatusActive))
}

func TestCompareAndSwapCodexTicketRejectsChangedFixedProxy(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	account := codexTicketCASAccount()
	setCodexTicketCASProxy(account)
	mock.ExpectBegin()
	expectCodexTicketCASProxy(mock, "changed.example")
	mock.ExpectRollback()
	changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", nil)
	require.NoError(t, err)
	require.False(t, changed)
	require.NoError(t, mock.ExpectationsWereMet())
}

type codexTicketCASSchedulerCache struct {
	service.SchedulerCache
	account *service.Account
}

func (cache *codexTicketCASSchedulerCache) SetAccount(_ context.Context, account *service.Account) error {
	cache.account = account
	return nil
}

func TestCompareAndSwapCodexTicketSyncsCommittedSnapshot(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	account := codexTicketCASAccount()
	setCodexTicketCASProxy(account)
	cache := &codexTicketCASSchedulerCache{}
	repo.schedulerCache = cache
	mock.ExpectBegin()
	expectCodexTicketCASProxy(mock, "proxy.example")
	expectCodexTicketCAS(mock).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	mock.ExpectQuery(`(?s)SELECT .* FROM "accounts"`).WithArgs(int64(41)).
		WillReturnRows(updatedAccountRows(41, `{"codex_turn_ticket:model":{"state":"new"},"other":"latest"}`))
	mock.ExpectQuery(`(?s)SELECT .* FROM "account_groups"`).WithArgs(int64(41)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "group_id", "priority", "created_at"}))
	changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", map[string]any{"state": "new"})
	require.NoError(t, err)
	require.True(t, changed)
	require.NotNil(t, cache.account)
	require.Equal(t, "latest", cache.account.Extra["other"], "cache must receive the complete fresh snapshot")
	require.Equal(t, map[string]any{"state": "new"}, cache.account.Extra["codex_turn_ticket:model"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCompareAndSwapCodexTicketUsesCallerTransactionWithoutPrematureSync(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	account := codexTicketCASAccount()
	setCodexTicketCASProxy(account)
	mock.ExpectBegin()
	tx, err := repo.client.Tx(context.Background())
	require.NoError(t, err)
	repo.client = nil // 外部事务提供 SQL client；若提前同步缓存，GetByID 会访问这个 nil client。
	repo.schedulerCache = &codexTicketCASSchedulerCache{}
	expectCodexTicketCASProxy(mock, "proxy.example")
	expectCodexTicketCAS(mock).WithArgs("codex_turn_ticket:model", "null", int64(41), service.PlatformOpenAI,
		service.AccountTypeOAuth, `{"access_token":"test"}`, int64(9), "null", codexTicketCASDefaultConfig).
		WillReturnResult(sqlmock.NewResult(0, 1))
	changed, err := repo.CompareAndSwapCodexTicket(dbent.NewTxContext(context.Background(), tx), account, "model", nil)
	require.NoError(t, err)
	require.True(t, changed)
	mock.ExpectRollback()
	require.NoError(t, tx.Rollback())
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCompareAndSwapCodexTicketPropagatesDatabaseErrors(t *testing.T) {
	for _, rowsError := range []bool{false, true} {
		t.Run(map[bool]string{false: "execute", true: "rows affected"}[rowsError], func(t *testing.T) {
			repo, mock := newCodexTicketCASRepo(t)
			want := errors.New("database unavailable")
			expectation := expectCodexTicketCAS(mock)
			if rowsError {
				expectation.WillReturnResult(sqlmock.NewErrorResult(want))
			} else {
				expectation.WillReturnError(want)
			}
			changed, err := repo.CompareAndSwapCodexTicket(context.Background(), codexTicketCASAccount(), "model", nil)
			require.ErrorIs(t, err, want)
			require.False(t, changed)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCompareAndSwapCodexTicketPublicationEligibilityKeepsRevocationAvailable(t *testing.T) {
	for _, reason := range []string{"disabled", "expired", "cooldown", "expiry allowed", "unschedulable"} {
		t.Run(reason, func(t *testing.T) {
			repo, mock := newCodexTicketCASRepo(t)
			account, now := codexTicketCASAccount(), time.Now().UTC()
			expired := now.Add(-time.Hour)
			switch reason {
			case "disabled":
				account.Status = service.StatusDisabled
			case "expired", "expiry allowed":
				account.AutoPauseOnExpired, account.ExpiresAt = reason == "expired", &expired
			case "cooldown":
				account.Extra[service.DailyCooldownExtraKey] = map[string]any{"enabled": true, "timezone": "UTC",
					"start": now.Add(-time.Hour).Format("15:04"), "end": now.Add(time.Hour).Format("15:04")}
			case "unschedulable":
				account.Schedulable = false
			}
			allowed := reason == "expiry allowed"
			if allowed {
				expectCodexTicketCAS(mock).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", map[string]any{"state": "new"})
			require.NoError(t, err)
			require.Equal(t, allowed, changed)
			expectCodexTicketCAS(mock).WillReturnResult(sqlmock.NewResult(0, 1))
			changed, err = repo.CompareAndSwapCodexTicket(context.Background(), account, "model", nil)
			require.NoError(t, err)
			require.True(t, changed, "stale tickets remain revocable")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestCompareAndSwapCodexTicketRejectsInvalidInputsBeforeSQL(t *testing.T) {
	for _, tc := range []struct {
		name        string
		change      func(*service.Account)
		model       string
		replacement any
	}{
		{"bad id", func(a *service.Account) { a.ID = 0 }, "model", nil},
		{"blank model", func(*service.Account) {}, " \t", nil},
		{"credentials", func(a *service.Account) { a.Credentials["invalid"] = make(chan int) }, "model", nil},
		{"expected ticket", func(a *service.Account) { a.Extra["codex_turn_ticket:model"] = make(chan int) }, "model", nil},
		{"configuration", func(a *service.Account) { a.Extra[service.ProxyModeExtraKey] = make(chan int) }, "model", nil},
		{"replacement", func(*service.Account) {}, "model", make(chan int)},
		{"incomplete proxy", func(a *service.Account) { setCodexTicketCASProxy(a); a.Proxy = nil }, "model", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo, mock := newCodexTicketCASRepo(t)
			account := codexTicketCASAccount()
			tc.change(account)
			changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, tc.model, tc.replacement)
			require.Error(t, err)
			require.False(t, changed)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
	repo, mock := newCodexTicketCASRepo(t)
	changed, err := repo.CompareAndSwapCodexTicket(context.Background(), nil, "model", nil)
	require.ErrorIs(t, err, service.ErrAccountNilInput)
	require.False(t, changed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCompareAndSwapCodexTicketTombstoneRemainsWritableForDisabledAccount(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	account := codexTicketCASAccount()
	account.Status = service.StatusDisabled
	account.Extra["codex_turn_ticket:model"] = map[string]any{"state": "old", "captured_at": "2026-09-20T01:00:00Z"}
	tombstone := map[string]any{"state": "old", "captured_at": "2026-09-20T01:00:00Z", "revoked": true}
	expectCodexTicketRevocation(mock, 1)
	changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", tombstone)
	require.NoError(t, err)
	require.True(t, changed)
	tombstone["revoked"] = "true"
	changed, err = repo.CompareAndSwapCodexTicket(context.Background(), account, "model", tombstone)
	require.NoError(t, err)
	require.False(t, changed, "only boolean true marks revocation")
	require.NoError(t, mock.ExpectationsWereMet())
}
