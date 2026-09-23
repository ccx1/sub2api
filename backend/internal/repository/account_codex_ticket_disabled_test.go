package repository

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func codexTicketUnavailableAccount(reason string) *service.Account {
	account := codexTicketCASAccount()
	switch reason {
	case "unschedulable":
		account.Schedulable = false
	case "shared paused":
		account.Extra[service.SharedPoolOwnerKey] = int64(7)
		account.Extra[service.SharedPoolEnabledKey] = false
	case "shared admin disabled":
		account.Extra[service.SharedPoolOwnerKey] = int64(7)
		account.Extra[service.SharedPoolEnabledKey] = true
		account.Extra[service.SharedPoolAdminDisabledKey] = true
	case "shared missing enablement":
		account.Extra[service.SharedPoolOwnerKey] = int64(7)
	}
	return account
}

func TestCodexTicketUnavailableAccountsCannotPublish(t *testing.T) {
	for _, reason := range []string{"unschedulable", "shared paused", "shared admin disabled", "shared missing enablement"} {
		t.Run(reason, func(t *testing.T) {
			repo, mock := newCodexTicketCASRepo(t)
			changed, err := repo.CompareAndSwapCodexTicket(context.Background(), codexTicketUnavailableAccount(reason), "model", map[string]any{"state": "new"})
			require.NoError(t, err)
			require.False(t, changed)
			require.NoError(t, mock.ExpectationsWereMet(), "不可用账号不应发起发布 SQL")
		})
	}
}

func TestCodexTicketUnavailableAccountsCanRevokeAndDelete(t *testing.T) {
	for _, reason := range []string{"unschedulable", "shared paused", "shared admin disabled", "shared missing enablement"} {
		t.Run(reason, func(t *testing.T) {
			repo, mock := newCodexTicketCASRepo(t)
			account := codexTicketUnavailableAccount(reason)
			account.Extra["codex_turn_ticket:model"] = map[string]any{"state": "old", "captured_at": "2026-09-20T01:00:00Z"}
			expectCodexTicketRevocation(mock, 1)
			changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model",
				map[string]any{"state": "old", "captured_at": "2026-09-20T01:00:00Z", "revoked": true})
			require.NoError(t, err)
			require.True(t, changed)
			expectCodexTicketCAS(mock).WillReturnResult(sqlmock.NewResult(0, 1))
			changed, err = repo.CompareAndSwapCodexTicket(context.Background(), account, "model", nil)
			require.NoError(t, err)
			require.True(t, changed)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

type codexTicketSharedCASConfig struct{}

func (codexTicketSharedCASConfig) Match(value driver.Value) bool {
	raw, ok := value.(string)
	var config map[string]any
	if !ok || json.Unmarshal([]byte(raw), &config) != nil {
		return false
	}
	return config[service.SharedPoolOwnerKey] == float64(7) &&
		config[service.SharedPoolEnabledKey] == true && config[service.SharedPoolAdminDisabledKey] == false
}

func TestCodexTicketConcurrentDisableRejectsPublication(t *testing.T) {
	repo, mock := newCodexTicketCASRepo(t)
	account := codexTicketCASAccount()
	account.Extra = map[string]any{service.SharedPoolOwnerKey: int64(7), service.SharedPoolEnabledKey: true, service.SharedPoolAdminDisabledKey: false}
	// 调度状态由数据库发布条件检查；共享开关通过完整旧配置快照进行 CAS。
	mock.ExpectExec(`(?s)UPDATE accounts.*jsonb_each\(\$9::jsonb\).*COALESCE\(extra -> expected.key, 'null'::jsonb\) <> expected.value.*status = 'active'.*schedulable = true`).
		WithArgs("codex_turn_ticket:model", `{"state":"new"}`, int64(41), service.PlatformOpenAI, service.AccountTypeOAuth,
			`{"access_token":"test"}`, nil, "null", codexTicketSharedCASConfig{}).WillReturnResult(sqlmock.NewResult(0, 0))
	changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", map[string]any{"state": "new"})
	require.NoError(t, err)
	require.False(t, changed, "数据库发现并发禁用时不得报告发布成功")
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestCodexTicketSharedStateChangesInvalidateCASConfig(t *testing.T) {
	for _, key := range []string{service.SharedPoolOwnerKey, service.SharedPoolEnabledKey, service.SharedPoolAdminDisabledKey} {
		t.Run(key, func(t *testing.T) {
			account := codexTicketCASAccount()
			account.Extra = map[string]any{service.SharedPoolOwnerKey: int64(7), service.SharedPoolEnabledKey: true, service.SharedPoolAdminDisabledKey: false}
			before, err := prepareCodexTicketCAS(account, "model", map[string]any{"state": "new"})
			require.NoError(t, err)
			if key == service.SharedPoolOwnerKey {
				account.Extra[key] = int64(8)
			} else {
				account.Extra[key] = !account.Extra[key].(bool)
			}
			after, err := prepareCodexTicketCAS(account, "model", map[string]any{"state": "new"})
			require.NoError(t, err)
			require.NotEqual(t, before.args[8], after.args[8], "共享状态必须参与原子发布的配置比较")
		})
	}
}

func TestCodexTicketTemporaryBusinessLimitsPermitRecoveryPublication(t *testing.T) {
	for _, reason := range []string{"temporary unschedulable", "rate limit", "overload"} {
		t.Run(reason, func(t *testing.T) {
			repo, mock := newCodexTicketCASRepo(t)
			account, until := codexTicketCASAccount(), time.Now().Add(time.Hour)
			switch reason {
			case "temporary unschedulable":
				account.TempUnschedulableUntil = &until
			case "rate limit":
				account.RateLimitResetAt = &until
			case "overload":
				account.OverloadUntil = &until
			}
			expectCodexTicketCAS(mock).WillReturnResult(sqlmock.NewResult(0, 1))
			changed, err := repo.CompareAndSwapCodexTicket(context.Background(), account, "model", map[string]any{"state": "new"})
			require.NoError(t, err)
			require.True(t, changed, "临时业务限制不能阻断已启用账号的恢复采集")
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
