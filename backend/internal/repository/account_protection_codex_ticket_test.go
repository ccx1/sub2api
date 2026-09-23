package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProtectionLockedMergePreservesLatestMatchingTicket(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer client.Close()
	account := &service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Concurrency: 4, UpdatedAt: time.Now().UTC(), Credentials: map[string]any{"chatgpt_account_id": "test-account"}, Extra: map[string]any{}}
	before := protectionLockedTicketBinding(t, account)
	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano)
	ticket := map[string]any{"account_binding": before, "verified": true, "model": "gpt-6-astra", "state": "gAAAAA" + strings.Repeat("B", 286),
		"expires_at": expires, "captured_at": time.Now().UTC().Format(time.RFC3339Nano), "future_metadata": "keep"}
	extra := map[string]any{"codex_ticket_enabled": true, "codex_turn_ticket:gpt-6-astra": ticket}
	raw, err := json.Marshal(extra)
	require.NoError(t, err)
	mock.ExpectQuery(`SELECT extra, updated_at FROM accounts`).WithArgs(account.ID).
		WillReturnRows(sqlmock.NewRows([]string{"extra", "updated_at"}).AddRow(raw, account.UpdatedAt))
	updated, err := service.NewAntiDegradeService(&protectionLockedStore{account: account, client: client}).SetProtection(context.Background(), 7, true, false)
	require.NoError(t, err)
	raw, err = json.Marshal(updated.Extra["codex_turn_ticket:gpt-6-astra"])
	require.NoError(t, err)
	var persisted map[string]any
	require.NoError(t, json.Unmarshal(raw, &persisted))
	require.NotEqual(t, before, persisted["account_binding"])
	require.Equal(t, expires, persisted["expires_at"])
	require.Equal(t, "keep", persisted["future_metadata"])
	require.Equal(t, ticket["state"], persisted["state"])
	require.True(t, service.OpenAICodexTicketAccountEnabled(updated))
	statuses := service.OpenAICodexTicketStatuses(updated, config.NormalizeOpenAICodexTicketConfig(config.OpenAICodexTicketConfig{Enabled: true}), time.Now())
	require.True(t, statuses[0].Ready, "必须使用完整账号身份和行锁内最新票据计算新绑定")
	require.NoError(t, mock.ExpectationsWereMet())
}

func protectionLockedTicketBinding(t *testing.T, account *service.Account) string {
	t.Helper()
	extra := map[string]any{"enable_tls_fingerprint": nil, "tls_fingerprint_builtin": nil, "tls_fingerprint_profile_id": nil,
		"codex_fingerprint_mode": nil, "anti_degrade": nil, "anti_degradation": nil}
	identity := map[string]any{"account": account.ID, "platform": account.Platform, "type": account.Type,
		"chatgpt_account_id": account.Credentials["chatgpt_account_id"], "organization_id": nil,
		"chatgpt_organization_id": nil, "random": false, "extra": extra, "proxy_id": (*int64)(nil), "proxy_url": ""}
	raw, err := json.Marshal(identity)
	require.NoError(t, err)
	digest := sha256.Sum256(raw)
	return "v2:" + hex.EncodeToString(digest[:])
}
