package repository

import (
	"context"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type protectionLockedStore struct {
	account *service.Account
	client  *dbent.Client
}

func (s *protectionLockedStore) GetAccount(context.Context, int64) (*service.Account, error) {
	return s.account, nil
}

func (s *protectionLockedStore) UpdateAccount(ctx context.Context, _ int64, input *service.UpdateAccountInput) (*service.Account, error) {
	account := *s.account
	account.Extra = input.Extra
	if input.Concurrency != nil {
		account.Concurrency = *input.Concurrency
	}
	if err := preserveLockedAccountProtection(ctx, s.client, &account); err != nil {
		return nil, err
	}
	return &account, nil
}

func TestProtectionTransitionChecksLockedRevision(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "same_revision", true: "concurrent_update"}[changed], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
			defer client.Close()
			original := time.Now().UTC().Truncate(time.Microsecond)
			revision := original
			if changed {
				revision = revision.Add(time.Second)
			}
			mock.ExpectQuery(`SELECT extra, updated_at FROM accounts`).WithArgs(int64(7)).WillReturnRows(
				sqlmock.NewRows([]string{"extra", "updated_at"}).AddRow([]byte(`{"random_proxy_last_used":{"proxy_id":19}}`), revision))
			store := &protectionLockedStore{account: &service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Concurrency: 4, UpdatedAt: original}, client: client}
			account, err := service.NewAntiDegradeService(store).SetProtection(context.Background(), 7, true, false)
			if changed {
				require.ErrorIs(t, err, service.ErrProtectionConflict)
			} else {
				require.NoError(t, err)
				require.True(t, account.AntiDegradationEnabled())
				require.Equal(t, map[string]any{"proxy_id": float64(19)}, account.Extra["random_proxy_last_used"])
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestLockedProtectionIgnoresStaleExtraAndKeepsLatestObservation(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer client.Close()
	mock.ExpectQuery(`SELECT extra FROM accounts`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"extra"}).AddRow(
		[]byte(`{"anti_degradation":true,"protection_scope":"generic_v1","anti_degrade":{"enabled":true,"mode":"generic","max_concurrency":4},"random_proxy_last_used":{"proxy_id":19}}`)))
	account := &service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Concurrency: 4, Extra: map[string]any{"anti_degradation": false, "random_proxy_last_used": map[string]any{"proxy_id": 1}, "custom": true}}
	require.NoError(t, preserveLockedAccountProtection(context.Background(), client, account))
	require.True(t, account.AntiDegradationEnabled())
	require.Equal(t, map[string]any{"proxy_id": float64(19)}, account.Extra["random_proxy_last_used"])
	require.Equal(t, true, account.Extra["custom"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestProtectionEnablePreservesTicketChoiceAfterLockedExtraMerge(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	defer client.Close()
	original := time.Now().UTC().Truncate(time.Microsecond)
	mock.ExpectQuery(`SELECT extra, updated_at FROM accounts`).WithArgs(int64(7)).WillReturnRows(
		sqlmock.NewRows([]string{"extra", "updated_at"}).AddRow([]byte(`{"codex_ticket_enabled":false,"custom":"keep"}`), original))
	store := &protectionLockedStore{account: &service.Account{
		ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Concurrency: 4, UpdatedAt: original,
		Extra: map[string]any{service.OpenAICodexTicketEnabledExtraKey: false, "custom": "keep"},
	}, client: client}
	account, err := service.NewAntiDegradeService(store).SetProtection(context.Background(), 7, true, false)
	require.NoError(t, err)
	require.True(t, account.AntiDegradationEnabled())
	require.False(t, service.OpenAICodexTicketAccountEnabled(account))
	require.Equal(t, false, account.Extra[service.OpenAICodexTicketEnabledExtraKey])
	require.Equal(t, "keep", account.Extra["custom"])
	require.NoError(t, mock.ExpectationsWereMet())
}
